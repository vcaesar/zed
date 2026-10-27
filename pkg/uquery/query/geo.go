/* Copyright 2022 Zinc Labs Inc. and Contributors
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

package query

import (
	"fmt"
	"strings"

	"github.com/vcaesar/riot"
	"github.com/vcaesar/riot/numeric/geo"

	"github.com/zincsearch/zincsearch/pkg/errors"
	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils"
)

// geoQueryOptions are the Elasticsearch option keys shared by geo queries;
// every other key is the field name.
var geoQueryOptions = map[string]bool{
	"boost": true, "_name": true, "distance": true, "distance_type": true,
	"validation_method": true, "ignore_unmapped": true,
}

// splitGeoQuery separates the geo field entry from the query options and checks the field type.
func splitGeoQuery(name string, query map[string]interface{}, mappings *meta.Mappings) (field string, value interface{}, boost float64, err error) {
	boost = -1.0
	for k, v := range query {
		k = strings.ToLower(k)
		switch {
		case k == "boost":
			if boost, err = zutils.ToFloat64(v); err != nil {
				return "", nil, 0, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] boost must be numeric", name))
			}
		case geoQueryOptions[k]:
			// handled by caller or ignored
		case field != "":
			return "", nil, 0, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] query doesn't support multiple fields", name))
		default:
			field, value = k, v
		}
	}
	if field == "" {
		return "", nil, 0, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[%s] query requires a geo_point field", name))
	}
	if prop, _ := mappings.GetProperty(field); prop.Type != "geo_point" {
		return "", nil, 0, errors.New(errors.ErrorTypeXContentParseException,
			fmt.Sprintf("[%s] field [%s] is not of type [geo_point], got %q", name, field, prop.Type))
	}
	return field, value, boost, nil
}

// GeoDistanceQuery parses:
//
//	{"distance": "12km", "location": {"lat": 40, "lon": -70}}
func GeoDistanceQuery(query map[string]interface{}, mappings *meta.Mappings) (riot.Query, error) {
	field, value, boost, err := splitGeoQuery("geo_distance", query, mappings)
	if err != nil {
		return nil, err
	}
	distance, err := zutils.ToString(query["distance"])
	if err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, "[geo_distance] requires distance")
	}
	if _, err := geo.ParseDistance(distance); err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_distance] invalid distance %q", distance))
	}
	center, err := meta.ParseGeoPoint(value)
	if err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_distance] %s", err))
	}
	subq := riot.NewGeoDistanceQuery(center.Lon, center.Lat, distance).SetField(field)
	if boost >= 0 {
		subq.SetBoost(boost)
	}
	return subq, nil
}

// GeoBoundingBoxQuery parses:
//
//	{"location": {"top_left": {"lat": 40.73, "lon": -74.1}, "bottom_right": {"lat": 40.01, "lon": -71.12}}}
//	{"location": {"top": 40.73, "left": -74.1, "bottom": 40.01, "right": -71.12}}
func GeoBoundingBoxQuery(query map[string]interface{}, mappings *meta.Mappings) (riot.Query, error) {
	field, value, boost, err := splitGeoQuery("geo_bounding_box", query, mappings)
	if err != nil {
		return nil, err
	}
	box, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_bounding_box] field [%s] must be an object", field))
	}
	topLeft, bottomRight, err := parseGeoBoundingBox(box)
	if err != nil {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_bounding_box] %s", err))
	}
	subq := riot.NewGeoBoundingBoxQuery(topLeft.Lon, topLeft.Lat, bottomRight.Lon, bottomRight.Lat).SetField(field)
	if boost >= 0 {
		subq.SetBoost(boost)
	}
	return subq, nil
}

func parseGeoBoundingBox(box map[string]interface{}) (topLeft, bottomRight meta.GeoPoint, err error) {
	corners := make(map[string]meta.GeoPoint)
	edges := make(map[string]float64)
	for k, v := range box {
		k = strings.ToLower(k)
		switch k {
		case "top_left", "bottom_right", "top_right", "bottom_left":
			if corners[k], err = meta.ParseGeoPoint(v); err != nil {
				return topLeft, bottomRight, fmt.Errorf("%s: %w", k, err)
			}
		case "top", "left", "bottom", "right":
			if edges[k], err = zutils.ToFloat64(v); err != nil {
				return topLeft, bottomRight, fmt.Errorf("%s must be numeric", k)
			}
		}
	}
	switch {
	case len(corners) == 2 && len(edges) == 0:
		var okTL, okBR bool
		topLeft, okTL = corners["top_left"]
		bottomRight, okBR = corners["bottom_right"]
		if okTL && okBR {
			return topLeft, bottomRight, nil
		}
		tr, okTR := corners["top_right"]
		bl, okBL := corners["bottom_left"]
		if okTR && okBL {
			return meta.GeoPoint{Lat: tr.Lat, Lon: bl.Lon}, meta.GeoPoint{Lat: bl.Lat, Lon: tr.Lon}, nil
		}
	case len(edges) == 4 && len(corners) == 0:
		return meta.GeoPoint{Lat: edges["top"], Lon: edges["left"]}, meta.GeoPoint{Lat: edges["bottom"], Lon: edges["right"]}, nil
	}
	return topLeft, bottomRight, fmt.Errorf("requires top_left/bottom_right, top_right/bottom_left or top/left/bottom/right")
}

// GeoPolygonQuery parses:
//
//	{"location": {"points": [{"lat": 40, "lon": -70}, [-80, 30], "20,-90"]}}
func GeoPolygonQuery(query map[string]interface{}, mappings *meta.Mappings) (riot.Query, error) {
	field, value, boost, err := splitGeoQuery("geo_polygon", query, mappings)
	if err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]interface{})
	if !ok {
		return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_polygon] field [%s] must be an object", field))
	}
	rawPoints, ok := obj["points"].([]interface{})
	if !ok || len(rawPoints) < 3 {
		return nil, errors.New(errors.ErrorTypeParsingException, "[geo_polygon] requires at least three points")
	}
	points := make([]geo.Point, 0, len(rawPoints))
	for _, raw := range rawPoints {
		p, err := meta.ParseGeoPoint(raw)
		if err != nil {
			return nil, errors.New(errors.ErrorTypeParsingException, fmt.Sprintf("[geo_polygon] %s", err))
		}
		points = append(points, geo.Point{Lon: p.Lon, Lat: p.Lat})
	}
	subq := riot.NewGeoBoundingPolygonQuery(points).SetField(field)
	if boost >= 0 {
		subq.SetBoost(boost)
	}
	return subq, nil
}

func GeoShapeQuery(query map[string]interface{}) (riot.Query, error) {
	return nil, errors.New(errors.ErrorTypeNotImplemented, "[geo_shape] query doesn't support")
}
