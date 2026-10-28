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

package aggregation

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vcaesar/riot/search"
	"github.com/vcaesar/riot/search/aggregations"

	zincaggregation "github.com/vcaesar/zedsearch/pkg/bluge/aggregation"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/errors"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/uquery/source"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

func Request(req zincaggregation.SearchAggregation, aggs map[string]meta.Aggregations, mappings *meta.Mappings) error {
	if len(aggs) == 0 {
		return nil // not need aggregation
	}
	if mappings == nil {
		return nil // mapping is empty
	}

	var err error
	// handle aggregation
	for name, agg := range aggs {
		switch {
		case agg.Avg != nil:
			req.AddAggregation(name, aggregations.Avg(search.Field(agg.Avg.Field)))
		case agg.WeightedAvg != nil:
			req.AddAggregation(name, aggregations.WeightedAvg(search.Field(agg.WeightedAvg.Field), search.Field(agg.WeightedAvg.WeightField)))
		case agg.Max != nil:
			req.AddAggregation(name, aggregations.Max(search.Field(agg.Max.Field)))
		case agg.Min != nil:
			req.AddAggregation(name, aggregations.Min(search.Field(agg.Min.Field)))
		case agg.Sum != nil:
			req.AddAggregation(name, aggregations.Sum(search.Field(agg.Sum.Field)))
		case agg.Count != nil:
			req.AddAggregation(name, aggregations.CountMatches())
		case agg.ValueCount != nil:
			prop, _ := mappings.GetProperty(agg.ValueCount.Field)
			if prop.Type != "numeric" {
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[value_count] aggregation doesn't support values of type: [%s:[%s]]", agg.ValueCount.Field, prop.Type),
				)
			}
			req.AddAggregation(name, zincaggregation.NewValueCountAggregation(
				search.Field(agg.ValueCount.Field),
				zincaggregation.NumericValuesSource,
			))
		case agg.Stats != nil:
			prop, _ := mappings.GetProperty(agg.Stats.Field)
			if prop.Type != "numeric" {
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[stats] aggregation doesn't support values of type: [%s:[%s]]", agg.Stats.Field, prop.Type),
				)
			}
			req.AddAggregation(name, zincaggregation.NewStatsAggregation(search.Field(agg.Stats.Field)))
		case agg.ExtendedStats != nil:
			prop, _ := mappings.GetProperty(agg.ExtendedStats.Field)
			if prop.Type != "numeric" {
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[extended_stats] aggregation doesn't support values of type: [%s:[%s]]", agg.ExtendedStats.Field, prop.Type),
				)
			}
			sigma := agg.ExtendedStats.Sigma
			if sigma == 0 {
				sigma = 2
			}
			if sigma < 0 {
				return errors.New(errors.ErrorTypeParsingException, "[extended_stats] aggregation sigma must not be negative")
			}
			req.AddAggregation(name, zincaggregation.NewExtendedStatsAggregation(search.Field(agg.ExtendedStats.Field), sigma))
		case agg.Percentiles != nil:
			prop, _ := mappings.GetProperty(agg.Percentiles.Field)
			if prop.Type != "numeric" {
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[percentiles] aggregation doesn't support values of type: [%s:[%s]]", agg.Percentiles.Field, prop.Type),
				)
			}
			percents := agg.Percentiles.Percents
			if len(percents) == 0 {
				percents = zincaggregation.DefaultPercents
			}
			for _, p := range percents {
				if p < 0 || p > 100 {
					return errors.New(errors.ErrorTypeParsingException, "[percentiles] aggregation percents must be in [0, 100]")
				}
			}
			req.AddAggregation(name, zincaggregation.NewPercentilesAggregation(search.Field(agg.Percentiles.Field), percents))
		case agg.Missing != nil:
			// missing accepts any field type, including unmapped fields
			req.AddAggregation(name, zincaggregation.NewMissingAggregation(search.Field(agg.Missing.Field)))
		case agg.Cardinality != nil:
			req.AddAggregation(name, aggregations.Cardinality(search.Field(agg.Cardinality.Field)))
		case agg.Terms != nil:
			if agg.Terms.Size == 0 {
				agg.Terms.Size = config.Global.AggregationTermsSize
			}
			var subreq *zincaggregation.TermsAggregation
			prop, _ := mappings.GetProperty(agg.Terms.Field)
			switch prop.Type {
			case "text", "keyword":
				subreq = zincaggregation.NewTermsAggregation(search.Field(agg.Terms.Field), zincaggregation.TextValueSource, agg.Terms.Size)
			case "numeric":
				subreq = zincaggregation.NewTermsAggregation(search.Field(agg.Terms.Field), zincaggregation.NumericValueSource, agg.Terms.Size)
			case "bool", "boolean":
				subreq = zincaggregation.NewTermsAggregation(search.Field(agg.Terms.Field), zincaggregation.BooleanValueSource, agg.Terms.Size)
			default:
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[terms] aggregation doesn't support values of type: [%s:[%s]]", agg.Terms.Field, prop.Type),
				)
			}
			if len(agg.Terms.Order) > 0 {
				key, asc, err := singleOrder("terms", agg.Terms.Order)
				if err != nil {
					return err
				}
				subreq.SetOrder(key, asc)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.Range != nil:
			if len(agg.Range.Ranges) == 0 {
				return errors.New(errors.ErrorTypeParsingException, "[range] aggregation needs ranges")
			}
			var subreq *aggregations.RangeAggregation
			prop, _ := mappings.GetProperty(agg.Range.Field)
			switch prop.Type {
			case "numeric":
				subreq = aggregations.Ranges(search.Field(agg.Range.Field))
				for _, v := range agg.Range.Ranges {
					subreq.AddRange(aggregations.Range(v.From, v.To))
				}
				req.AddAggregation(name, subreq)
			default:
				return errors.New(errors.ErrorTypeParsingException, "[range] aggregation only support type numeric")
			}
		case agg.DateRange != nil:
			if len(agg.DateRange.Ranges) == 0 {
				return errors.New(errors.ErrorTypeParsingException, "[date_range] aggregation needs ranges")
			}
			var subreq *aggregations.DateRangeAggregation
			format := time.RFC3339
			prop, ok := mappings.GetProperty(agg.DateRange.Field)
			if ok {
				if prop.Format != "" {
					format = prop.Format
				}
			}
			if agg.DateRange.Format != "" {
				format = agg.DateRange.Format
			}
			timeZone := time.UTC
			if agg.DateRange.TimeZone != "" {
				timeZone, err = zutils.ParseTimeZone(agg.DateRange.TimeZone)
				if err != nil {
					return errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[date_range] time_zone parse err %s", err.Error()))
				}
			}
			switch prop.Type {
			case "date", "time":
				subreq = aggregations.DateRanges(search.Field(agg.DateRange.Field))
				for _, v := range agg.DateRange.Ranges {
					from := time.Time{}
					to := time.Time{}
					if v.From != "" {
						from, err = time.ParseInLocation(format, v.From, timeZone)
						if err != nil {
							return errors.New(errors.ErrorTypeIllegalArgumentException, fmt.Sprintf("[date_range] range value from parse err %s", err.Error()))
						}
					}
					if v.To != "" {
						to, err = time.ParseInLocation(format, v.To, timeZone)
						if err != nil {
							return errors.New(errors.ErrorTypeIllegalArgumentException, fmt.Sprintf("[date_range] range value to parse err %s", err.Error()))
						}
					}
					subreq.AddRange(aggregations.NewDateRange(from, to))
				}
				req.AddAggregation(name, subreq)
			default:
				return errors.New(errors.ErrorTypeParsingException, "[date_range] aggregation only support type datetime")
			}
		case agg.Histogram != nil:
			if agg.Histogram.Size == 0 {
				agg.Histogram.Size = config.Global.AggregationTermsSize
			}
			if agg.Histogram.Interval <= 0 {
				return errors.New(errors.ErrorTypeParsingException, "[histogram] aggregation interval must be a positive decimal")
			}
			if agg.Histogram.Offset >= agg.Histogram.Interval {
				return errors.New(errors.ErrorTypeParsingException, "[histogram] aggregation offset must be in [0, interval)")
			}
			var subreq *zincaggregation.HistogramAggregation
			prop, _ := mappings.GetProperty(agg.Histogram.Field)
			switch prop.Type {
			case "numeric":
				subreq = zincaggregation.NewHistogramAggregation(
					search.Field(agg.Histogram.Field),
					agg.Histogram.Interval,
					agg.Histogram.Offset,
					agg.Histogram.ExtendedBounds,
					agg.Histogram.HardBounds,
					agg.Histogram.MinDocCount,
					agg.Histogram.Size,
				)
			default:
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf("[histogram] aggregation doesn't support values of type: [%s:[%s]]", agg.Histogram.Field, prop.Type),
				)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.DateHistogram != nil:
			if agg.DateHistogram.Size == 0 {
				agg.DateHistogram.Size = config.Global.AggregationTermsSize
			}
			if agg.DateHistogram.Interval != "" {
				agg.DateHistogram.FixedInterval = agg.DateHistogram.Interval
			}
			if agg.DateHistogram.CalendarInterval == "" && agg.DateHistogram.FixedInterval == "" {
				return errors.New(errors.ErrorTypeParsingException, "[date_histogram] aggregation calendar_interval or fixed_interval must be set one")
			}

			// format interval
			var interval int64
			calendarInterval := agg.DateHistogram.CalendarInterval
			if agg.DateHistogram.CalendarInterval != "" {
				switch agg.DateHistogram.CalendarInterval {
				case "second", "1s":
					interval = int64(time.Second)
					calendarInterval = ""
				case "minute", "1m":
					interval = int64(time.Minute)
					calendarInterval = ""
				case "hour", "1h":
					interval = int64(time.Hour)
					calendarInterval = ""
				case "day", "1d":
					interval = int64(time.Hour * 24)
					calendarInterval = ""
				case "week", "1w", "month", "1M", "quarter", "1q", "year", "1y":
					// calendar
				default:
					return errors.New(
						errors.ErrorTypeParsingException,
						"[date_histogram] aggregation calendar_interval must be Date Calendar, such as: second, minute, hour, day, week, month, quarter, year",
					)
				}
			} else if agg.DateHistogram.FixedInterval != "" {
				if duration, err := zutils.ParseDuration(agg.DateHistogram.FixedInterval); err != nil {
					return errors.New(errors.ErrorTypeParsingException, "[date_histogram] aggregation fixed_interval must be time duration, such as: 1s, 1m, 1h, 1d")
				} else {
					interval = int64(duration)
				}
			}

			timeZone := time.UTC
			if agg.DateHistogram.TimeZone != "" {
				timeZone, err = zutils.ParseTimeZone(agg.DateHistogram.TimeZone)
				if err != nil {
					return errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[date_histogram] time_zone parse err %s", err.Error()))
				}
			}
			if agg.DateHistogram.Format == "" {
				agg.DateHistogram.Format = time.RFC3339
			}
			var subreq *zincaggregation.DateHistogramAggregation
			prop, _ := mappings.GetProperty(agg.DateHistogram.Field)
			switch prop.Type {
			case "date", "time":
				subreq = zincaggregation.NewDateHistogramAggregation(
					search.Field(agg.DateHistogram.Field),
					calendarInterval,
					interval,
					agg.DateHistogram.Format,
					timeZone,
					agg.DateHistogram.ExtendedBounds,
					agg.DateHistogram.HardBounds,
					agg.DateHistogram.MinDocCount,
					agg.DateHistogram.Size,
				)
			default:
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf(
						"[date_histogram] aggregation doesn't support values of type: [%s:[%s]]",
						agg.DateHistogram.Field,
						prop.Type,
					),
				)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.AutoDateHistogram != nil:
			if agg.AutoDateHistogram.Buckets <= 0 {
				agg.AutoDateHistogram.Buckets = 10
			}
			if agg.AutoDateHistogram.MinimumInterval == "" {
				agg.AutoDateHistogram.MinimumInterval = "second"
			}
			if agg.AutoDateHistogram.MinimumInterval != "" {
				switch agg.AutoDateHistogram.MinimumInterval {
				case "second", "minute", "hour", "day", "month", "year":
					// calendar
				default:
					return errors.New(
						errors.ErrorTypeParsingException,
						"[auto_date_histogram] aggregation minimum_interval must be Date Calendar, such as: second, minute, hour, day, month, year",
					)
				}
			}

			timeZone := time.UTC
			if agg.AutoDateHistogram.TimeZone != "" {
				timeZone, err = zutils.ParseTimeZone(agg.AutoDateHistogram.TimeZone)
				if err != nil {
					return errors.New(errors.ErrorTypeXContentParseException, fmt.Sprintf("[auto_date_histogram] time_zone parse err %s", err.Error()))
				}
			}
			if agg.AutoDateHistogram.Format == "" {
				agg.AutoDateHistogram.Format = time.RFC3339
			}
			var subreq *zincaggregation.AutoDateHistogramAggregation
			prop, _ := mappings.GetProperty(agg.AutoDateHistogram.Field)
			switch prop.Type {
			case "date", "time":
				subreq = zincaggregation.NewAutoDateHistogramAggregation(
					search.Field(agg.AutoDateHistogram.Field),
					agg.AutoDateHistogram.Buckets,
					agg.AutoDateHistogram.MinimumInterval,
					agg.AutoDateHistogram.Format,
					timeZone,
				)
			default:
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf(
						"[auto_date_histogram] aggregation doesn't support values of type: [%s:[%s]]",
						agg.AutoDateHistogram.Field,
						prop.Type,
					),
				)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.Filter != nil:
			pred, err := FilterPredicate("filter", agg.Filter, mappings)
			if err != nil {
				return err
			}
			subreq := zincaggregation.NewFilterAggregation(pred)
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.Filters != nil:
			if len(agg.Filters.Filters) == 0 {
				return errors.New(errors.ErrorTypeParsingException, "[filters] aggregation needs filters")
			}
			subreq := zincaggregation.NewFiltersAggregation(otherBucketKey(agg.Filters))
			filterNames := make([]string, 0, len(agg.Filters.Filters))
			for filterName := range agg.Filters.Filters {
				filterNames = append(filterNames, filterName)
			}
			sort.Strings(filterNames) // the request order of a json object is lost
			for _, filterName := range filterNames {
				pred, err := FilterPredicate("filters", agg.Filters.Filters[filterName], mappings)
				if err != nil {
					return err
				}
				subreq.AddFilter(filterName, pred)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.Global != nil:
			return errors.New(
				errors.ErrorTypeNotImplemented,
				"[global] aggregation doesn't support, it requires a second match_all search",
			)
		case agg.MultiTerms != nil:
			if len(agg.MultiTerms.Terms) < 2 {
				return errors.New(errors.ErrorTypeParsingException, "[multi_terms] aggregation needs at least two fields")
			}
			if agg.MultiTerms.Size == 0 {
				agg.MultiTerms.Size = config.Global.AggregationTermsSize
			}
			srcs := make([]search.FieldSource, 0, len(agg.MultiTerms.Terms))
			valueTypes := make([]int, 0, len(agg.MultiTerms.Terms))
			for _, term := range agg.MultiTerms.Terms {
				prop, _ := mappings.GetProperty(term.Field)
				switch prop.Type {
				case "text", "keyword":
					valueTypes = append(valueTypes, zincaggregation.TextValueSource)
				case "numeric":
					valueTypes = append(valueTypes, zincaggregation.NumericValueSource)
				case "bool", "boolean":
					valueTypes = append(valueTypes, zincaggregation.BooleanValueSource)
				default:
					return errors.New(
						errors.ErrorTypeParsingException,
						fmt.Sprintf("[multi_terms] aggregation doesn't support values of type: [%s:[%s]]", term.Field, prop.Type),
					)
				}
				srcs = append(srcs, search.Field(term.Field))
			}
			subreq := zincaggregation.NewMultiTermsAggregation(srcs, valueTypes, agg.MultiTerms.Size)
			if len(agg.MultiTerms.Order) > 0 {
				key, asc, err := singleOrder("multi_terms", agg.MultiTerms.Order)
				if err != nil {
					return err
				}
				subreq.SetOrder(key, asc)
			}
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.TopHits != nil:
			subreq, err := topHitsAggregation(agg.TopHits)
			if err != nil {
				return err
			}
			req.AddAggregation(name, subreq)
		case agg.PercentileRanks != nil:
			prop, _ := mappings.GetProperty(agg.PercentileRanks.Field)
			if prop.Type != "numeric" {
				return errors.New(
					errors.ErrorTypeParsingException,
					fmt.Sprintf(
						"[percentile_ranks] aggregation doesn't support values of type: [%s:[%s]]",
						agg.PercentileRanks.Field, prop.Type,
					),
				)
			}
			if len(agg.PercentileRanks.Values) == 0 {
				return errors.New(errors.ErrorTypeParsingException, "[percentile_ranks] aggregation needs values")
			}
			req.AddAggregation(name, zincaggregation.NewPercentileRanksAggregation(
				search.Field(agg.PercentileRanks.Field),
				agg.PercentileRanks.Values,
			))
		case agg.Nested != nil:
			if agg.Nested.Path == "" {
				return errors.New(errors.ErrorTypeParsingException, "[nested] aggregation needs path")
			}
			subreq := zincaggregation.NewNestedAggregation(meta.NestedPathField, agg.Nested.Path)
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.ReverseNested != nil:
			subreq := zincaggregation.NewReverseNestedAggregation(meta.NestedPathField)
			if len(agg.Aggregations) > 0 {
				if err := Request(subreq, agg.Aggregations, mappings); err != nil {
					return err
				}
			}
			req.AddAggregation(name, subreq)
		case agg.IPRange != nil:
			return errors.New(errors.ErrorTypeNotImplemented, "[ip_range] aggregation doesn't support")
		default:
			// pipeline aggregations read the response tree, see ApplyPipelines
		}
	}

	return nil
}

// otherBucketKey returns the key of the [filters] bucket holding the
// documents that match no filter, "" when it was not requested. As in
// elasticsearch other_bucket_key implies other_bucket, and other_bucket alone
// uses the default key.
func otherBucketKey(agg *meta.AggregationFilters) string {
	if agg.OtherBucketKey != "" {
		return agg.OtherBucketKey
	}
	if agg.OtherBucket {
		return meta.DefaultOtherBucketKey
	}
	return ""
}

// singleOrder reads the single {key: asc|desc} entry of an aggregation order.
func singleOrder(kind string, order map[string]string) (string, bool, error) {
	if len(order) > 1 {
		return "", false, errors.New(errors.ErrorTypeParsingException,
			fmt.Sprintf("[%s] aggregation order supports a single key", kind))
	}
	for key, dir := range order {
		switch dir {
		case "asc":
			return key, true, nil
		case "desc":
			return key, false, nil
		default:
			return "", false, errors.New(errors.ErrorTypeParsingException,
				fmt.Sprintf("[%s] aggregation order must be asc or desc, got [%s]", kind, dir))
		}
	}
	return zincaggregation.OrderByCount, false, nil
}

// topHitsAggregation builds the [top_hits] aggregation.
//
// Deviation from elasticsearch: only a single sort key is supported, and the
// hits carry _id, _index, _score and _source but no sort values.
func topHitsAggregation(agg *meta.AggregationTopHits) (*zincaggregation.TopHitsAggregation, error) {
	if agg.Size < 0 || agg.From < 0 {
		return nil, errors.New(errors.ErrorTypeParsingException, "[top_hits] aggregation size and from must not be negative")
	}
	size := agg.Size
	if size == 0 {
		size = 3 // elasticsearch default
	}
	sortField, sortDesc, err := topHitsSort(agg.Sort)
	if err != nil {
		return nil, err
	}
	src, err := source.Request(agg.Source)
	if err != nil {
		return nil, err
	}

	rv := zincaggregation.NewTopHitsAggregation(size, agg.From, sortField, sortDesc)
	rv.SetSource(src.Enable, src.Fields)
	return rv, nil
}

// topHitsSort accepts "field", "-field", ["field"], [{"field": "desc"}] and
// [{"field": {"order": "desc"}}]. An empty field sorts by descending _score.
func topHitsSort(v interface{}) (field string, desc bool, err error) {
	switch v := v.(type) {
	case nil:
		return "", true, nil
	case []interface{}:
		if len(v) == 0 {
			return "", true, nil
		}
		if len(v) > 1 {
			return "", false, errors.New(errors.ErrorTypeNotImplemented, "[top_hits] aggregation supports a single sort key")
		}
		return topHitsSort(v[0])
	case string:
		if v == "_score" {
			return "", true, nil
		}
		switch {
		case strings.HasPrefix(v, "-"):
			return v[1:], true, nil
		case strings.HasPrefix(v, "+"):
			return v[1:], false, nil
		default:
			return v, false, nil
		}
	case map[string]interface{}:
		if len(v) != 1 {
			return "", false, errors.New(errors.ErrorTypeNotImplemented, "[top_hits] aggregation supports a single sort key")
		}
		for key, order := range v {
			asc, err := sortOrder(order)
			if err != nil {
				return "", false, errors.New(errors.ErrorTypeParsingException,
					fmt.Sprintf("[top_hits] aggregation sort order must be asc or desc, got [%v]", order))
			}
			if key == "_score" {
				return "", !asc, nil
			}
			return key, !asc, nil
		}
	}
	return "", false, errors.New(errors.ErrorTypeXContentParseException, "[top_hits] aggregation sort format error")
}

func Response(bucket *search.Bucket) (map[string]meta.AggregationResponse, error) {
	resp := make(map[string]meta.AggregationResponse)
	aggs := bucket.Aggregations()
	for name, v := range aggs {
		switch v := v.(type) {
		case *zincaggregation.StatsCalculator:
			resp[name] = statsResponse(v)
		case *zincaggregation.PercentilesCalculator:
			resp[name] = meta.AggregationResponse{Values: v.Values()}
		case *zincaggregation.MissingCalculator:
			docCount := v.DocCount()
			resp[name] = meta.AggregationResponse{DocCount: &docCount}
		case *zincaggregation.PercentileRanksCalculator:
			if err := v.Err(); err != nil {
				return nil, errors.New(errors.ErrorTypeRuntimeException,
					fmt.Sprintf("[%s] percentile_ranks aggregation err %s", name, err.Error()))
			}
			resp[name] = meta.AggregationResponse{Values: v.Values()}
		case *zincaggregation.TopHitsCalculator:
			hits, err := topHitsResponse(name, v)
			if err != nil {
				return nil, err
			}
			resp[name] = meta.AggregationResponse{Hits: hits}
		case *zincaggregation.FilterCalculator:
			aggResp, err := singleBucketResponse(v.Bucket())
			if err != nil {
				return nil, err
			}
			resp[name] = aggResp
		case *zincaggregation.FiltersCalculator:
			buckets := make(map[string]interface{}, 0)
			for _, b := range v.NamedBuckets() {
				aggResp, err := singleBucketResponse(b)
				if err != nil {
					return nil, err
				}
				buckets[b.Name()] = aggResp
			}
			resp[name] = meta.AggregationResponse{Buckets: buckets}
		case *zincaggregation.MultiTermsCalculator:
			aggRespBuckets := make([]map[string]interface{}, 0, len(v.Buckets()))
			for _, bucket := range v.Buckets() {
				aggBucket := map[string]interface{}{
					"key":           v.Key(bucket),
					"key_as_string": bucket.Name(),
					"doc_count":     bucket.Count(),
				}
				if err := addSubAggregations(aggBucket, bucket); err != nil {
					return nil, err
				}
				aggRespBuckets = append(aggRespBuckets, aggBucket)
			}
			resp[name] = meta.AggregationResponse{Buckets: aggRespBuckets}
		case search.MetricCalculator:
			// An empty bucket yields NaN (avg) or +/-Inf (min, max): json has
			// no such values, elasticsearch reports null and zinc reports 0.
			f := v.Value()
			if math.IsNaN(f) || math.IsInf(f, 0) {
				f = 0
			}
			resp[name] = meta.AggregationResponse{Value: f}
		case search.DurationCalculator:
			resp[name] = meta.AggregationResponse{Value: v.Duration().Milliseconds()}
		case search.BucketCalculator:
			buckets := v.Buckets()
			aggResp := meta.AggregationResponse{Buckets: make([]map[string]interface{}, 0)}
			aggRespBuckets := make([]map[string]interface{}, 0)
			for _, bucket := range buckets {
				aggBucket := map[string]interface{}{"key": bucket.Name(), "doc_count": bucket.Count()}
				if zutils.IsNumeric(bucket.Name()) {
					key, _ := strconv.ParseInt(bucket.Name(), 10, 64)
					aggBucket["key"] = key
					aggBucket["key_as_string"] = bucket.Name()
				}
				if err := addSubAggregations(aggBucket, bucket); err != nil {
					return nil, err
				}
				aggRespBuckets = append(aggRespBuckets, aggBucket)
			}
			aggResp.Buckets = aggRespBuckets

			// hack: auto_date_histogram aggregation
			if v, ok := aggs[name].(*zincaggregation.AutoDateHistogramCalculator); ok {
				aggResp.Interval = v.Interval()
			}

			resp[name] = aggResp
		default:
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s:%T] aggregation doesn't support", name, v))
		}
	}

	return resp, nil
}

// addSubAggregations renders the sub-aggregations of a bucket next to its key
// and doc_count. "count" is the internal doc counter, it is never returned.
func addSubAggregations(aggBucket map[string]interface{}, bucket *search.Bucket) error {
	if len(bucket.Aggregations()) <= 1 {
		return nil
	}
	subResp, err := Response(bucket)
	if err != nil {
		return err
	}
	delete(subResp, "count")
	for k, v := range subResp {
		aggBucket[k] = v
	}
	return nil
}

// singleBucketResponse renders the one bucket of [filter] / [filters] /
// [nested]: doc_count plus the sub-aggregations, which MarshalJSON splices
// into the same object.
func singleBucketResponse(bucket *search.Bucket) (meta.AggregationResponse, error) {
	docCount := bucket.Count()
	rv := meta.AggregationResponse{DocCount: &docCount}
	if len(bucket.Aggregations()) <= 1 {
		return rv, nil
	}
	subResp, err := Response(bucket)
	if err != nil {
		return rv, err
	}
	delete(subResp, "count")
	rv.Aggregations = subResp
	return rv, nil
}

// topHitsResponse renders the documents kept by [top_hits].
func topHitsResponse(name string, c *zincaggregation.TopHitsCalculator) (*meta.AggregationHits, error) {
	if err := c.Err(); err != nil {
		return nil, errors.New(errors.ErrorTypeRuntimeException,
			fmt.Sprintf("[%s] top_hits aggregation err %s", name, err.Error()))
	}
	enable, fields := c.Source()
	src := &meta.Source{Enable: enable, Fields: fields}

	hits := c.Hits()
	rv := &meta.AggregationHits{
		Total:    meta.Total{Value: int(c.Total())},
		MaxScore: c.MaxScore(),
		Hits:     make([]meta.AggregationHit, 0, len(hits)),
	}
	for i := range hits {
		hit := meta.AggregationHit{
			Index: hits[i].Index,
			Type:  "_doc",
			ID:    hits[i].ID,
			Score: hits[i].Score,
		}
		if len(hits[i].Source) > 0 {
			hit.Source = source.Response(src, hits[i].Source)
		}
		rv.Hits = append(rv.Hits, hit)
	}
	return rv, nil
}

// statsResponse renders [stats], plus the extra [extended_stats] fields when
// the calculator was built for it.
func statsResponse(c *zincaggregation.StatsCalculator) meta.AggregationResponse {
	count := c.Count()
	min, max, avg, sum := finite(c.Min()), finite(c.Max()), finite(c.Avg()), finite(c.Sum())
	rv := meta.AggregationResponse{
		Count: &count,
		Min:   &min,
		Max:   &max,
		Avg:   &avg,
		Sum:   &sum,
	}
	if !c.Extended() {
		return rv
	}

	sumOfSquares, variance, stdDeviation := finite(c.SumOfSquares()), finite(c.Variance()), finite(c.StdDeviation())
	rv.SumOfSquares = &sumOfSquares
	rv.Variance = &variance
	rv.StdDeviation = &stdDeviation
	rv.StdDeviationBounds = &meta.AggregationStdDeviationBounds{
		Upper: finite(avg + c.Sigma()*stdDeviation),
		Lower: finite(avg - c.Sigma()*stdDeviation),
	}
	return rv
}

// finite maps NaN and +/-Inf, which json cannot encode, to 0. Huge values
// overflow the stats sums to Inf and the variance to NaN.
func finite(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}
