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

package meta

import (
	"sort"
	"time"

	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// SearchResponse for a query
type SearchResponse struct {
	Took         int                            `json:"took"` // Time it took to generate the response
	TimedOut     bool                           `json:"timed_out"`
	Shards       Shards                         `json:"_shards"`
	Hits         Hits                           `json:"hits"`
	Aggregations map[string]AggregationResponse `json:"aggregations,omitempty"`
	Error        string                         `json:"error,omitempty"`
}

type Shards struct {
	Total      int64 `json:"total"`
	Successful int64 `json:"successful"`
	Skipped    int64 `json:"skipped"`
	Failed     int64 `json:"failed"`
}

type Hits struct {
	Total    Total   `json:"total"`
	MaxScore float64 `json:"max_score"`
	Hits     []Hit   `json:"hits"`
}

type Hit struct {
	Index     string                 `json:"_index"`
	Type      string                 `json:"_type"`
	ID        string                 `json:"_id"`
	Score     float64                `json:"_score"`
	Timestamp time.Time              `json:"@timestamp"`
	Source    interface{}            `json:"_source,omitempty"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Highlight map[string]interface{} `json:"highlight,omitempty"`
}

type Total struct {
	Value int `json:"value"` // Count of documents returned
}

type AggregationResponse struct {
	Value    interface{} `json:"value,omitempty"`
	Buckets  interface{} `json:"buckets,omitempty"`  // slice or map
	Interval string      `json:"interval,omitempty"` // support for auto_date_histogram_aggregation

	// stats aggregation; pointers so that a zero count / min still serializes
	Count *uint64  `json:"count,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`
	Avg   *float64 `json:"avg,omitempty"`
	Sum   *float64 `json:"sum,omitempty"`

	// extended_stats aggregation, in addition to the stats fields above
	SumOfSquares       *float64                       `json:"sum_of_squares,omitempty"`
	Variance           *float64                       `json:"variance,omitempty"`
	StdDeviation       *float64                       `json:"std_deviation,omitempty"`
	StdDeviationBounds *AggregationStdDeviationBounds `json:"std_deviation_bounds,omitempty"`

	// percentiles aggregation, keyed form only: {"1.0": v, ...}.
	// Elasticsearch returns an array of {key, value} objects when keyed is
	// false, zed always returns the keyed map.
	Values map[string]float64 `json:"values,omitempty"`

	// missing / filter / filters / nested aggregations
	DocCount *uint64 `json:"doc_count,omitempty"`

	// top_hits aggregation
	Hits *AggregationHits `json:"hits,omitempty"`

	// Aggregations holds the sub-aggregations of a single bucket (filter,
	// filters entry, nested). Elasticsearch renders them as siblings of
	// doc_count, so MarshalJSON splices them into the same object.
	Aggregations map[string]AggregationResponse `json:"-"`
}

// MarshalJSON renders the sub-aggregations of a single bucket inline, next to
// the fields of the aggregation itself.
func (a AggregationResponse) MarshalJSON() ([]byte, error) {
	type plain AggregationResponse
	buf, err := json.Marshal(plain(a))
	if err != nil {
		return nil, err
	}
	if len(a.Aggregations) == 0 {
		return buf, nil
	}

	names := make([]string, 0, len(a.Aggregations))
	for name := range a.Aggregations {
		names = append(names, name)
	}
	sort.Strings(names)

	buf = buf[:len(buf)-1] // drop the closing brace
	for _, name := range names {
		if len(buf) > 1 {
			buf = append(buf, ',')
		}
		key, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(a.Aggregations[name])
		if err != nil {
			return nil, err
		}
		buf = append(buf, key...)
		buf = append(buf, ':')
		buf = append(buf, value...)
	}
	return append(buf, '}'), nil
}

// AggregationHits is the top_hits aggregation payload.
type AggregationHits struct {
	Total    Total            `json:"total"`
	MaxScore float64          `json:"max_score"`
	Hits     []AggregationHit `json:"hits"`
}

// AggregationHit is one document returned by top_hits.
type AggregationHit struct {
	Index  string      `json:"_index,omitempty"`
	Type   string      `json:"_type,omitempty"`
	ID     string      `json:"_id"`
	Score  float64     `json:"_score"`
	Source interface{} `json:"_source,omitempty"`
}

type AggregationStdDeviationBounds struct {
	Upper float64 `json:"upper"`
	Lower float64 `json:"lower"`
}
