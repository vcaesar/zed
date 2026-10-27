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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vcaesar/riot"

	"github.com/zincsearch/zincsearch/pkg/meta"
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

func geoMappings() *meta.Mappings {
	m := meta.NewMappings()
	m.SetProperty("location", meta.NewProperty("geo_point"))
	m.SetProperty("name", meta.NewProperty("text"))
	return m
}

func geoQuery(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	q := make(map[string]interface{})
	assert.NoError(t, json.Unmarshal([]byte(s), &q))
	return q
}

func TestGeoDistanceQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "object point", query: `{"distance": "12km", "location": {"lat": 40, "lon": -70}}`},
		{name: "array point with boost", query: `{"distance": "200mi", "boost": 2, "location": [-70, 40]}`},
		{name: "string point", query: `{"distance": "1000", "location": "40,-70"}`},
		{name: "numeric distance", query: `{"distance": 1000, "location": "40,-70"}`},
		{name: "missing distance", query: `{"location": "40,-70"}`, wantErr: true},
		{name: "bad distance", query: `{"distance": "far", "location": "40,-70"}`, wantErr: true},
		{name: "bad point", query: `{"distance": "1km", "location": "?"}`, wantErr: true},
		{name: "not geo field", query: `{"distance": "1km", "name": "40,-70"}`, wantErr: true},
		{name: "no field", query: `{"distance": "1km"}`, wantErr: true},
		{name: "multiple fields", query: `{"distance": "1km", "location": "40,-70", "name": "40,-70"}`, wantErr: true},
		{name: "bad boost", query: `{"distance": "1km", "boost": "x", "location": "40,-70"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GeoDistanceQuery(geoQuery(t, tt.query), geoMappings())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			q, ok := got.(*riot.GeoDistanceQuery)
			assert.True(t, ok)
			assert.Equal(t, "location", q.Field())
			assert.Equal(t, []float64{-70, 40}, q.Location())
		})
	}

	t.Run("boost", func(t *testing.T) {
		got, err := GeoDistanceQuery(geoQuery(t, `{"distance": "1km", "boost": 2, "location": "40,-70"}`), geoMappings())
		assert.NoError(t, err)
		assert.Equal(t, 2.0, got.(*riot.GeoDistanceQuery).Boost())
	})
}

func TestGeoBoundingBoxQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "top_left bottom_right", query: `{"location": {"top_left": {"lat": 40.73, "lon": -74.1}, "bottom_right": {"lat": 40.01, "lon": -71.12}}}`},
		{name: "top_right bottom_left", query: `{"location": {"top_right": [-71.12, 40.73], "bottom_left": [-74.1, 40.01]}}`},
		{name: "top left bottom right", query: `{"location": {"top": 40.73, "left": -74.1, "bottom": 40.01, "right": -71.12}}`},
		{name: "mixed corners", query: `{"location": {"top_left": "40.73,-74.1", "bottom_left": "40.01,-74.1"}}`, wantErr: true},
		{name: "missing corner", query: `{"location": {"top_left": "40.73,-74.1"}}`, wantErr: true},
		{name: "bad corner", query: `{"location": {"top_left": "?", "bottom_right": "40.01,-71.12"}}`, wantErr: true},
		{name: "bad edge", query: `{"location": {"top": "x", "left": -74.1, "bottom": 40.01, "right": -71.12}}`, wantErr: true},
		{name: "not object", query: `{"location": "40,-70"}`, wantErr: true},
		{name: "not geo field", query: `{"name": {"top_left": "40.73,-74.1", "bottom_right": "40.01,-71.12"}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GeoBoundingBoxQuery(geoQuery(t, tt.query), geoMappings())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			q, ok := got.(*riot.GeoBoundingBoxQuery)
			assert.True(t, ok)
			assert.Equal(t, "location", q.Field())
			assert.Equal(t, []float64{-74.1, 40.73}, q.TopLeft())
			assert.Equal(t, []float64{-71.12, 40.01}, q.BottomRight())
		})
	}
}

func TestGeoPolygonQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{name: "mixed points", query: `{"location": {"points": [{"lat": 40, "lon": -70}, [-80, 30], "20,-90"]}}`},
		{name: "boost", query: `{"boost": 1.5, "location": {"points": ["40,-70", "30,-80", "20,-90"]}}`},
		{name: "too few points", query: `{"location": {"points": ["40,-70", "30,-80"]}}`, wantErr: true},
		{name: "no points", query: `{"location": {}}`, wantErr: true},
		{name: "bad point", query: `{"location": {"points": ["40,-70", "30,-80", "?"]}}`, wantErr: true},
		{name: "not object", query: `{"location": "40,-70"}`, wantErr: true},
		{name: "not geo field", query: `{"name": {"points": ["40,-70", "30,-80", "20,-90"]}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GeoPolygonQuery(geoQuery(t, tt.query), geoMappings())
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			q, ok := got.(*riot.GeoBoundingPolygonQuery)
			assert.True(t, ok)
			assert.Equal(t, "location", q.Field())
			points := q.Points()
			assert.Len(t, points, 3)
			assert.Equal(t, -70.0, points[0].Lon)
			assert.Equal(t, 40.0, points[0].Lat)
			assert.Equal(t, -90.0, points[2].Lon)
			assert.Equal(t, 20.0, points[2].Lat)
		})
	}
}

func TestGeoShapeQuery(t *testing.T) {
	_, err := GeoShapeQuery(map[string]interface{}{})
	assert.Error(t, err)
}

func TestQuery_Geo(t *testing.T) {
	q := geoQuery(t, `{"bool": {"filter": [{"geo_distance": {"distance": "10km", "location": "40,-70"}}]}}`)
	got, err := Query(q, geoMappings(), nil)
	assert.NoError(t, err)
	assert.NotNil(t, got)
}
