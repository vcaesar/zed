/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
 */

package core

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/analysis"
	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/highlight"

	zedsearch "github.com/vcaesar/zedsearch/pkg/bluge/search"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/uquery"
	"github.com/vcaesar/zedsearch/pkg/uquery/fields"
	uquerydsl "github.com/vcaesar/zedsearch/pkg/uquery/query"
	"github.com/vcaesar/zedsearch/pkg/uquery/source"
	"github.com/vcaesar/zedsearch/pkg/uquery/timerange"
)

func (index *Index) Search(query *meta.ZedQuery) (*meta.SearchResponse, error) {
	mappings := index.GetMappings()
	analyzers := index.GetAnalyzers()

	timeMin, timeMax := timerange.Query(query.Query)
	readers, err := index.GetReaders(timeMin, timeMax)
	if err != nil {
		log.Printf("index.SearchV2: error accessing reader: %s", err.Error())
		return nil, err
	}
	defer func() {
		for _, reader := range readers {
			reader.Close()
		}
	}()

	// nested and join queries are resolved before query is parsed, and hidden
	// nested documents are excluded from results.
	if err := rewriteJoinQuery(query, mappings, analyzers, readers); err != nil {
		return nil, err
	}

	if _, err = uquery.ParseQueryDSL(query, mappings, analyzers); err != nil {
		return nil, err
	}

	ctx := context.Background()
	var cancel context.CancelFunc
	if query.Timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), time.Duration(query.Timeout)*time.Second)
		defer cancel()
	}

	// dmi, err := riot.MultiSearch(ctx, searchRequest, readers...)
	dmi, err := zedsearch.MultiSearch(ctx, query, mappings, analyzers, readers...)
	if err != nil {
		log.Printf("index.SearchV2: error executing search: %s", err.Error())
		if err == context.DeadlineExceeded {
			return &meta.SearchResponse{
				TimedOut: true,
				Error:    err.Error(),
				Hits:     meta.Hits{Hits: []meta.Hit{}},
			}, nil
		}
		return nil, err
	}

	return searchV2(index.GetAllShardNum(), int64(len(readers)), dmi, query, mappings)
}

func searchV2(shardNum, readerNum int64, dmi search.DocumentMatchIterator, query *meta.ZedQuery, mappings *meta.Mappings) (*meta.SearchResponse, error) {
	resp := &meta.SearchResponse{
		Hits: meta.Hits{Hits: []meta.Hit{}},
	}

	// highlight
	var highlighter *highlight.SimpleHighlighter
	if query.Highlight != nil {
		if len(query.Highlight.PreTags) > 0 && len(query.Highlight.PostTags) > 0 {
			highlighter = highlight.NewHTMLHighlighterTags(query.Highlight.PreTags[0], query.Highlight.PostTags[0])
		} else {
			highlighter = highlight.NewHTMLHighlighter()
		}
	}

	Hits := make([]meta.Hit, 0)
	next, err := dmi.Next()
	for err == nil && next != nil {
		var id string
		var indexName string
		var timestamp time.Time
		var sourceData map[string]interface{}
		var fieldsData map[string]interface{}
		var highlightData map[string]interface{}
		if query.Highlight != nil {
			highlightData = make(map[string]interface{})
		}
		err = next.VisitStoredFields(func(field string, value []byte) bool {
			switch field {
			case "_id":
				id = string(value)
			case "_index":
				indexName = string(value)
			case "@timestamp":
				timestamp, _ = riot.DecodeDateTime(value)
			case "_source":
				sourceData = source.Response(query.Source.(*meta.Source), value)
				if query.Fields != nil {
					fieldsData = fields.Response(query.Fields.([]*meta.Field), value, mappings)
				}
			default:
				// highlight
				if query.Highlight != nil && query.Highlight.Fields != nil {
					if options, ok := query.Highlight.Fields[field]; ok {
						if v, ok := next.Locations[field]; ok {
							if len(options.PreTags) > 0 && len(options.PostTags) > 0 {
								highlighter := highlight.NewHTMLHighlighterTags(options.PreTags[0], options.PostTags[0])
								highlightData[field] = highlighter.BestFragments(v, value, options.NumberOfFragments)
							} else {
								highlightData[field] = highlighter.BestFragments(v, value, options.NumberOfFragments)
							}
						}
					}
				}
			}

			return true
		})
		if err != nil {
			log.Printf("core.SearchV2: error accessing stored fields: %s", err.Error())
			continue
		}

		if query.Source.(*meta.Source) == nil || !query.Source.(*meta.Source).Enable || len(query.Source.(*meta.Source).Fields) == 0 {
			sourceData["@timestamp"] = timestamp
		}

		hit := meta.Hit{
			Index:     indexName,
			Type:      "_doc",
			ID:        id,
			Score:     next.Score,
			Timestamp: timestamp,
			Source:    sourceData,
			Fields:    fieldsData,
			Highlight: highlightData,
		}
		Hits = append(Hits, hit)

		next, err = dmi.Next()
	}
	if err != nil {
		log.Printf("core.SearchV2: error iterating results: %s", err.Error())
	}

	resp.Took = int(dmi.Aggregations().Duration().Milliseconds())
	resp.Shards = meta.Shards{Total: shardNum, Successful: readerNum, Skipped: shardNum - readerNum}
	resp.Hits = meta.Hits{
		Total:    meta.Total{Value: int(dmi.Aggregations().Count())},
		MaxScore: dmi.Aggregations().Metric("max_score"),
		Hits:     Hits,
	}

	if err := uquery.FormatResponse(resp, query, dmi.Aggregations()); err != nil {
		log.Printf("core.SearchV2: error format response: %s", err.Error())
	}

	return resp, nil
}

// rewriteJoinQuery resolves nested and join clauses of query and hides nested
// documents from results.
//
// Riot has no block join support: nested array elements and join children are
// indexed as own documents, so `nested`, `has_child` and `has_parent` clauses
// are resolved by extra search over same readers, and result is pushed back
// into query as list of document ids (see uquery.Rewrite).
func rewriteJoinQuery(query *meta.ZedQuery, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer, readers []*riot.Reader) error {
	paths := nestedPaths(mappings)
	if len(paths) == 0 {
		if _, ok := mappings.GetProperty(meta.JoinNameField); !ok {
			return nil // no nested or join mapping, nothing to rewrite
		}
	}

	exec := func(sub map[string]interface{}, collectField string) ([]string, error) {
		return collectFieldValues(readers, sub, collectField, mappings, analyzers)
	}
	rewritten, err := uquery.Rewrite(query.Query, exec)
	if err != nil {
		return err
	}

	query.Query = excludeNestedDocuments(rewritten, paths)

	return nil
}

// excludeNestedDocuments wraps query so that hidden nested documents are
// neither returned nor counted in hits.total.
func excludeNestedDocuments(query interface{}, paths []string) interface{} {
	if len(paths) == 0 {
		return query
	}
	if query == nil {
		query = map[string]interface{}{"match_all": map[string]interface{}{}}
	}
	return map[string]interface{}{
		"bool": map[string]interface{}{
			"must": query,
			"must_not": map[string]interface{}{
				"terms": map[string]interface{}{meta.NestedPathField: paths},
			},
		},
	}
}

// collectFieldValues runs sub query over readers and returns value of field
// for every match, one entry per match. Hidden documents are not excluded,
// sub query searches them on purpose.
func collectFieldValues(readers []*riot.Reader, sub map[string]interface{}, field string, mappings *meta.Mappings, analyzers map[string]*analysis.Analyzer) ([]string, error) {
	subQuery, err := uquerydsl.Query(sub, mappings, analyzers)
	if err != nil {
		return nil, err
	}

	limit := uquery.RewriteLimit() + 1
	ctx := context.Background()
	values := make([]string, 0, 16)
	for _, reader := range readers {
		request := riot.NewTopNSearch(limit, subQuery)
		if field != "_id" {
			// sorting by collected field makes collector load its doc values
			request.SortBy([]string{field})
		}
		dmi, err := reader.Search(ctx, request)
		if err != nil {
			return nil, err
		}
		next, err := dmi.Next()
		for err == nil && next != nil {
			if field == "_id" {
				var id string
				if verr := next.VisitStoredFields(func(name string, value []byte) bool {
					if name == "_id" {
						id = string(value)
						return false
					}
					return true
				}); verr != nil {
					return nil, verr
				}
				if id != "" {
					values = append(values, id)
				}
			} else {
				for _, value := range next.DocValues(field) {
					values = append(values, string(value))
				}
			}
			next, err = dmi.Next()
		}
		if err != nil {
			return nil, err
		}
	}

	return values, nil
}
