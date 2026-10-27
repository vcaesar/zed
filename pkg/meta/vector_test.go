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

package meta

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseVector(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		dims    int
		want    []float32
		wantErr bool
	}{
		{name: "interface array", value: []interface{}{1.0, 2.0, 3.0}, want: []float32{1, 2, 3}},
		{name: "interface ints", value: []interface{}{1, int64(2), 3.5}, want: []float32{1, 2, 3.5}},
		{name: "float32 array", value: []float32{1, 2}, want: []float32{1, 2}},
		{name: "float64 array", value: []float64{1, 2}, want: []float32{1, 2}},
		{name: "dims match", value: []interface{}{1.0, 2.0}, dims: 2, want: []float32{1, 2}},
		{name: "dims mismatch", value: []interface{}{1.0, 2.0}, dims: 3, wantErr: true},
		{name: "empty", value: []interface{}{}, wantErr: true},
		{name: "not numeric", value: []interface{}{"a"}, wantErr: true},
		{name: "nan", value: []float64{math.NaN()}, wantErr: true},
		{name: "inf", value: []float64{math.Inf(1)}, wantErr: true},
		{name: "unsupported type", value: "1,2,3", wantErr: true},
		{name: "unsupported scalar", value: 1.0, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseVector(tt.value, tt.dims)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewProperty_Vector(t *testing.T) {
	p := NewProperty("vector")
	p.Dims = 3
	assert.False(t, p.Sortable)
	assert.False(t, p.Aggregatable)
	clone := p.DeepClone()
	assert.Equal(t, 3, clone.Dims)
}
