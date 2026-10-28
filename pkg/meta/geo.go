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
	"fmt"
	"strconv"
	"strings"

	"github.com/vcaesar/riot/numeric/geo"

	"github.com/vcaesar/zedsearch/pkg/zutils"
)

const geoHashAlphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

// GeoPoint is the canonical value of a geo_point field.
type GeoPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// ParseGeoPoint accepts the Elasticsearch geo_point formats:
//
//	{"lat": 41.12, "lon": -71.34}
//	{"type": "Point", "coordinates": [-71.34, 41.12]}
//	[-71.34, 41.12]
//	"41.12,-71.34"
//	"POINT (-71.34 41.12)"
//	"drm3btev3e86" (geohash)
func ParseGeoPoint(v interface{}) (GeoPoint, error) {
	var p GeoPoint
	var err error
	switch v := v.(type) {
	case GeoPoint:
		p = v
	case string:
		p, err = parseGeoPointString(v)
	case []interface{}:
		p, err = parseGeoPointCoordinates(v)
	case map[string]interface{}:
		if coords, ok := v["coordinates"]; ok {
			c, ok := coords.([]interface{})
			if !ok {
				return p, fmt.Errorf("invalid geo_point coordinates %v", coords)
			}
			p, err = parseGeoPointCoordinates(c)
		} else {
			p, err = parseGeoPointMap(v)
		}
	default:
		return p, fmt.Errorf("unsupported geo_point value type %T", v)
	}
	if err != nil {
		return p, err
	}
	if p.Lat < -90 || p.Lat > 90 {
		return p, fmt.Errorf("latitude %v out of range [-90, 90]", p.Lat)
	}
	if p.Lon < -180 || p.Lon > 180 {
		return p, fmt.Errorf("longitude %v out of range [-180, 180]", p.Lon)
	}
	return p, nil
}

// parseGeoPointCoordinates parses the GeoJSON [lon, lat] form.
func parseGeoPointCoordinates(c []interface{}) (GeoPoint, error) {
	if len(c) != 2 {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %v, expected [lon, lat]", c)
	}
	lon, err := zutils.ToFloat64(c[0])
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %v, expected [lon, lat]", c)
	}
	lat, err := zutils.ToFloat64(c[1])
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %v, expected [lon, lat]", c)
	}
	return GeoPoint{Lat: lat, Lon: lon}, nil
}

func parseGeoPointMap(m map[string]interface{}) (GeoPoint, error) {
	lat, err := zutils.ToFloat64(m["lat"])
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %v, expected {lat, lon}", m)
	}
	lonVal, ok := m["lon"]
	if !ok {
		lonVal = m["lng"]
	}
	lon, err := zutils.ToFloat64(lonVal)
	if err != nil {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %v, expected {lat, lon}", m)
	}
	return GeoPoint{Lat: lat, Lon: lon}, nil
}

func parseGeoPointString(s string) (GeoPoint, error) {
	s = strings.TrimSpace(s)
	if latStr, lonStr, ok := strings.Cut(s, ","); ok {
		lat, err := strconv.ParseFloat(strings.TrimSpace(latStr), 64)
		if err != nil {
			return GeoPoint{}, fmt.Errorf("invalid geo_point %q, expected \"lat,lon\"", s)
		}
		lon, err := strconv.ParseFloat(strings.TrimSpace(lonStr), 64)
		if err != nil {
			return GeoPoint{}, fmt.Errorf("invalid geo_point %q, expected \"lat,lon\"", s)
		}
		return GeoPoint{Lat: lat, Lon: lon}, nil
	}
	if wkt, ok := strings.CutPrefix(strings.ToUpper(s), "POINT"); ok {
		wkt = strings.TrimSpace(strings.Trim(strings.TrimSpace(wkt), "()"))
		lonStr, latStr, _ := strings.Cut(wkt, " ")
		lon, err := strconv.ParseFloat(strings.TrimSpace(lonStr), 64)
		if err != nil {
			return GeoPoint{}, fmt.Errorf("invalid geo_point %q, expected \"POINT (lon lat)\"", s)
		}
		lat, err := strconv.ParseFloat(strings.TrimSpace(latStr), 64)
		if err != nil {
			return GeoPoint{}, fmt.Errorf("invalid geo_point %q, expected \"POINT (lon lat)\"", s)
		}
		return GeoPoint{Lat: lat, Lon: lon}, nil
	}
	if s == "" || len(s) > 12 || strings.Trim(s, geoHashAlphabet) != "" {
		return GeoPoint{}, fmt.Errorf("invalid geo_point %q, expected geohash", s)
	}
	lat, lon := geo.DecodeGeoHash(s)
	return GeoPoint{Lat: lat, Lon: lon}, nil
}
