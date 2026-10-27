/* Copyright 2026 AtomAI
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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseGeoPoint(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    GeoPoint
		wantErr bool
	}{
		{name: "object", value: map[string]interface{}{"lat": 41.12, "lon": -71.34}, want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "object lng", value: map[string]interface{}{"lat": 41.12, "lng": -71.34}, want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "object int", value: map[string]interface{}{"lat": int64(41), "lon": 1}, want: GeoPoint{Lat: 41, Lon: 1}},
		{name: "geojson", value: map[string]interface{}{"type": "Point", "coordinates": []interface{}{-71.34, 41.12}}, want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "array", value: []interface{}{-71.34, 41.12}, want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "string", value: "41.12,-71.34", want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "string spaces", value: " 41.12 , -71.34 ", want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "wkt", value: "POINT (-71.34 41.12)", want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "wkt lower", value: "point(-71.34 41.12)", want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "geohash", value: "drm3btev3e86", want: GeoPoint{Lat: 41.12, Lon: -71.34}},
		{name: "geopoint", value: GeoPoint{Lat: 1, Lon: 2}, want: GeoPoint{Lat: 1, Lon: 2}},
		{name: "array wrong len", value: []interface{}{1.0}, wantErr: true},
		{name: "array not numeric", value: []interface{}{"a", "b"}, wantErr: true},
		{name: "object missing lon", value: map[string]interface{}{"lat": 1.0}, wantErr: true},
		{name: "geojson bad coordinates", value: map[string]interface{}{"type": "Point", "coordinates": "x"}, wantErr: true},
		{name: "string bad", value: "a,b", wantErr: true},
		{name: "wkt bad", value: "POINT (x y)", wantErr: true},
		{name: "geohash bad chars", value: "abc-def", wantErr: true},
		{name: "empty string", value: "", wantErr: true},
		{name: "lat out of range", value: "91,0", wantErr: true},
		{name: "lon out of range", value: "0,181", wantErr: true},
		{name: "unsupported type", value: 1.0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGeoPoint(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.InDelta(t, tt.want.Lat, got.Lat, 0.0001)
			assert.InDelta(t, tt.want.Lon, got.Lon, 0.0001)
		})
	}
}
