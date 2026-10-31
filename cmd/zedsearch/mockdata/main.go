/* Copyright 2026 AtomAI. and Contributors
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

// Command mockdata writes deterministic mock documents straight into the
// configured data path (zed_data_path), without going through the HTTP API.
// Each dataset exercises a different index feature: mapped text/keyword,
// vectors, the logs profile, geo_point, nested and join.
//
//	cd cmd/zedsearch && go run ./mockdata -reset
package main

import (
	"errors"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/core"
	indexhandler "github.com/vcaesar/zedsearch/pkg/handlers/index"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/metadata"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

var (
	categories = []string{"books", "electronics", "garden", "toys", "sports"}
	adjectives = []string{"Smart", "Classic", "Portable", "Eco", "Pro", "Mini"}
	nouns      = []string{"Lamp", "Speaker", "Backpack", "Kettle", "Drone", "Novel"}
	tags       = []string{"new", "sale", "bestseller", "gift", "limited", "refurbished"}
	levels     = []string{"debug", "info", "info", "info", "warn", "error"}
	services   = []string{"api", "auth", "billing", "search", "worker"}
	hosts      = []string{"node-1", "node-2", "node-3"}
	messages   = []string{"request handled", "cache miss", "user login", "payment retry", "index flushed", "timeout calling upstream"}
	cities     = []struct {
		name     string
		lat, lon float64
	}{
		{"San Francisco", 37.7749, -122.4194},
		{"Oakland", 37.8044, -122.2712},
		{"San Jose", 37.3382, -121.8863},
		{"Palo Alto", 37.4419, -122.1430},
		{"Berkeley", 37.8715, -122.2730},
	}
	topics   = []string{"indexing", "mappings", "aggregations", "vectors", "geo search", "cluster"}
	users    = []string{"alice", "bob", "carol", "dave", "erin"}
	comments = []string{"thanks, that helped", "same issue here", "works on v1.0", "please add an example", "fixed by reindexing"}
)

// dataset is one mock index: creation body (as for POST /api/index) and a
// deterministic document generator, which gets the prefixed index name.
type dataset struct {
	name  string
	index string // IndexSimple JSON without name
	gen   func(name string, n int, end time.Time) []map[string]any
}

var datasets = []dataset{
	{"products", `{"mappings": {"properties": {
		"name":      {"type": "text", "highlightable": true, "fields": {"keyword": {"type": "keyword"}}},
		"category":  {"type": "keyword"},
		"tags":      {"type": "keyword"},
		"price":     {"type": "float"},
		"stock":     {"type": "integer"},
		"in_stock":  {"type": "boolean"},
		"rating":    {"type": "float"},
		"embedding": {"type": "dense_vector", "dims": 4, "similarity": "cosine", "index_options": {"type": "hnsw"}}
	}}}`, products},
	{"logs", `{"settings": {"index_type": "logs"}, "mappings": {"properties": {
		"status":      {"type": "integer"},
		"duration_ms": {"type": "integer"}
	}}}`, logs},
	{"stores", `{"mappings": {"properties": {
		"name":     {"type": "text"},
		"city":     {"type": "keyword"},
		"location": {"type": "geo_point"},
		"open":     {"type": "boolean"},
		"rating":   {"type": "float"}
	}}}`, stores},
	{"forum", `{"mappings": {"properties": {
		"title":    {"type": "text", "highlightable": true},
		"topic":    {"type": "keyword"},
		"author":   {"type": "keyword"},
		"votes":    {"type": "integer"},
		"comments": {"type": "nested", "properties": {
			"user":  {"type": "keyword"},
			"text":  {"type": "text"},
			"stars": {"type": "integer"}
		}},
		"qa": {"type": "join", "relations": {"question": ["answer"]}}
	}}}`, forum},
}

// stamp spaces doc i of n by step so the last one lands on end, keeping mock
// data inside the console's default relative time range.
func stamp(end time.Time, n, i int, step time.Duration) string {
	return end.Add(-time.Duration(n-1-i) * step).Format(time.RFC3339)
}

func round(f float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(f*p) / p
}

// docID is the stable id of doc i (0-based) in index name.
func docID(name string, i int) string {
	return fmt.Sprintf("%s-%d", name, i+1)
}

func products(_ string, n int, end time.Time) []map[string]any {
	r := rand.New(rand.NewPCG(1, 2))
	docs := make([]map[string]any, n)
	for i := range docs {
		c := r.IntN(len(categories))
		// vectors cluster by category so knn finds similar products
		embedding := make([]any, 4)
		for d := range embedding {
			v := r.Float64() * 0.3
			if d == c%4 {
				v += 1
			}
			embedding[d] = round(v, 3)
		}
		docs[i] = map[string]any{
			"name":       adjectives[r.IntN(len(adjectives))] + " " + nouns[r.IntN(len(nouns))],
			"category":   categories[c],
			"tags":       []any{tags[r.IntN(len(tags))], tags[r.IntN(len(tags))]},
			"price":      float64(r.IntN(50000)) / 100,
			"stock":      r.IntN(500),
			"in_stock":   r.IntN(4) != 0,
			"rating":     float64(10+r.IntN(41)) / 10,
			"embedding":  embedding,
			"@timestamp": stamp(end, n, i, time.Hour),
		}
	}
	return docs
}

func logs(_ string, n int, end time.Time) []map[string]any {
	r := rand.New(rand.NewPCG(3, 4))
	docs := make([]map[string]any, n)
	for i := range docs {
		docs[i] = map[string]any{
			"level":       levels[r.IntN(len(levels))],
			"service":     map[string]any{"name": services[r.IntN(len(services))]},
			"host":        map[string]any{"name": hosts[r.IntN(len(hosts))]},
			"message":     messages[r.IntN(len(messages))],
			"status":      []int{200, 200, 200, 201, 400, 404, 500}[r.IntN(7)],
			"duration_ms": r.IntN(2000),
			"trace_id":    fmt.Sprintf("%016x", r.Uint64()),
			"@timestamp":  stamp(end, n, i, time.Minute),
		}
	}
	return docs
}

func stores(_ string, n int, end time.Time) []map[string]any {
	r := rand.New(rand.NewPCG(5, 6))
	docs := make([]map[string]any, n)
	for i := range docs {
		city := cities[r.IntN(len(cities))]
		lat := round(city.lat+(r.Float64()-0.5)*0.1, 4)
		lon := round(city.lon+(r.Float64()-0.5)*0.1, 4)
		// cycle through every geo_point input format
		var location any
		switch i % 4 {
		case 0:
			location = map[string]any{"lat": lat, "lon": lon}
		case 1:
			location = []any{lon, lat}
		case 2:
			location = fmt.Sprintf("%v,%v", lat, lon)
		default:
			location = map[string]any{"type": "Point", "coordinates": []any{lon, lat}}
		}
		docs[i] = map[string]any{
			"name":       fmt.Sprintf("%s %s Store #%d", city.name, nouns[r.IntN(len(nouns))], i+1),
			"city":       city.name,
			"location":   location,
			"open":       r.IntN(5) != 0,
			"rating":     float64(10+r.IntN(41)) / 10,
			"@timestamp": stamp(end, n, i, time.Hour),
		}
	}
	return docs
}

// forum generates questions (every 4th doc, starting with the first) with
// nested comments, each followed by answers joined to it.
func forum(name string, n int, end time.Time) []map[string]any {
	r := rand.New(rand.NewPCG(7, 8))
	docs := make([]map[string]any, n)
	question := ""
	for i := range docs {
		topic := topics[r.IntN(len(topics))]
		doc := map[string]any{
			"author":     users[r.IntN(len(users))],
			"topic":      topic,
			"votes":      r.IntN(100),
			"@timestamp": stamp(end, n, i, 10*time.Minute),
		}
		if i%4 == 0 {
			question = docID(name, i)
			doc["title"] = "How to configure " + topic + "?"
			doc["qa"] = "question"
			list := make([]any, 1+r.IntN(3))
			for j := range list {
				list[j] = map[string]any{
					"user":  users[r.IntN(len(users))],
					"text":  comments[r.IntN(len(comments))],
					"stars": 1 + r.IntN(5),
				}
			}
			doc["comments"] = list
		} else {
			doc["title"] = "Answer about " + topic
			doc["qa"] = map[string]any{"name": "answer", "parent": question}
		}
		docs[i] = doc
	}
	return docs
}

// create creates index name from d unless it already exists.
func create(name string, d dataset) error {
	if _, ok := core.GetIndex(name); ok {
		return nil
	}
	var req meta.IndexSimple
	if err := json.Unmarshal([]byte(d.index), &req); err != nil {
		return fmt.Errorf("index %s: %w", name, err)
	}
	req.Name = name
	if err := indexhandler.CreateIndexWorker(&req, ""); err != nil {
		return fmt.Errorf("index %s: %w", name, err)
	}
	return nil
}

const batch = 1000

// seed upserts docs into index name with stable ids, so reruns don't duplicate,
// and waits until the WAL is consumed into the index.
func seed(name string, docs []map[string]any) error {
	index, ok := core.GetIndex(name)
	if !ok {
		return fmt.Errorf("index %s not found", name)
	}
	stamps := make([]string, len(docs))
	for i, doc := range docs {
		stamps[i] = doc["@timestamp"].(string) // CreateDocument consumes @timestamp
		if err := index.CreateDocument(docID(name, i), doc, true); err != nil {
			return fmt.Errorf("index %s doc %d: %w", name, i+1, err)
		}
	}

	deadline := time.Now().Add(time.Minute)
	for {
		done, err := indexed(index, name, stamps)
		if err != nil {
			return fmt.Errorf("index %s search: %w", name, err)
		}
		if done == len(docs) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("index %s: %d/%d docs indexed before timeout", name, done, len(docs))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// indexed counts docs whose expected @timestamp is searchable, so reruns that
// only update existing ids are also waited for.
func indexed(index *core.Index, name string, stamps []string) (int, error) {
	done := 0
	for start := 0; start < len(stamps); start += batch {
		want := make(map[string]string, batch)
		ids := make([]string, 0, batch)
		for i := start; i < min(start+batch, len(stamps)); i++ {
			id := docID(name, i)
			ids = append(ids, id)
			want[id] = stamps[i]
		}
		res, err := index.Search(&meta.ZedQuery{Query: &meta.Query{Ids: &meta.IdsQuery{Values: ids}}, Size: len(ids)})
		if err != nil {
			return 0, err
		}
		for _, hit := range res.Hits.Hits {
			if hit.Timestamp.UTC().Format(time.RFC3339) == want[hit.ID] {
				done++
			}
		}
	}
	return done, nil
}

// run seeds every dataset as prefix+name with counts[name] docs. reset drops
// existing indexes first so mapping changes take effect.
func run(prefix string, counts map[string]int, end time.Time, reset bool) error {
	for _, d := range datasets {
		name := prefix + d.name
		if _, ok := core.GetIndex(name); ok && reset {
			if err := core.DeleteIndex(name); err != nil {
				return fmt.Errorf("index %s: %w", name, err)
			}
		}
		if err := create(name, d); err != nil {
			return err
		}
		if err := seed(name, d.gen(name, counts[d.name], end)); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	prefix := flag.String("prefix", "mock_", "index name prefix")
	reset := flag.Bool("reset", false, "delete existing mock indexes before seeding")
	counts := map[string]*int{
		"products": flag.Int("products", 200, "number of product documents"),
		"logs":     flag.Int("logs", 1000, "number of log documents"),
		"stores":   flag.Int("stores", 50, "number of store documents"),
		"forum":    flag.Int("forum", 120, "number of forum documents"),
	}
	flag.Parse()

	n := make(map[string]int, len(counts))
	for k, v := range counts {
		n[k] = *v
	}
	err := run(*prefix, n, time.Now().UTC().Truncate(time.Minute), *reset)
	if cerr := errors.Join(core.ZED_INDEX_LIST.Close(), metadata.Close()); cerr != nil {
		err = errors.Join(err, cerr)
	}
	if err != nil {
		log.Fatal().Err(err).Msg("mockdata")
	}
	log.Info().Msgf("mock data written with prefix %q: %v", *prefix, n)
}
