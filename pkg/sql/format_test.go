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

package sql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testResponse() *Response {
	return &Response{
		Columns: []ResultColumn{
			{Name: "level", Type: "keyword"},
			{Name: "COUNT(*)", Type: "long"},
			{Name: "AVG(price)", Type: "double"},
		},
		Rows: [][]interface{}{
			{"error", int64(3), 1.5},
			{"warn", int64(12), nil},
		},
	}
}

func TestResponseRender(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   string
	}{
		{
			name:   "json",
			format: FormatJSON,
			want: `{"columns":[{"name":"level","type":"keyword"},{"name":"COUNT(*)","type":"long"},` +
				`{"name":"AVG(price)","type":"double"}],"rows":[["error",3,1.5],["warn",12,null]]}`,
		},
		{
			name:   "txt",
			format: FormatTXT,
			want: "level|COUNT(*)|AVG(price)\n" +
				"-----+--------+----------\n" +
				"error|3       |1.5       \n" +
				"warn |12      |null      \n",
		},
		{
			name:   "csv",
			format: FormatCSV,
			want: "level,COUNT(*),AVG(price)\n" +
				"error,3,1.5\n" +
				"warn,12,\n",
		},
		{
			name:   "tsv",
			format: FormatTSV,
			want: "level\tCOUNT(*)\tAVG(price)\n" +
				"error\t3\t1.5\n" +
				"warn\t12\t\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := testResponse().Render(tt.format)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}
}

func TestResponseRenderQuoting(t *testing.T) {
	resp := &Response{
		Columns: []ResultColumn{{Name: "message", Type: "text"}},
		Rows:    [][]interface{}{{"a,b"}, {"a\tb"}},
	}

	data, err := resp.Render(FormatCSV)
	require.NoError(t, err)
	assert.Equal(t, "message\n\"a,b\"\na\tb\n", string(data))

	data, err = resp.Render(FormatTSV)
	require.NoError(t, err)
	assert.Equal(t, "message\na,b\na\\tb\n", string(data))
}

func TestResponseRenderUnknownFormat(t *testing.T) {
	_, err := testResponse().Render("yaml")
	require.Error(t, err)
	var sqlErr *Error
	assert.ErrorAs(t, err, &sqlErr)
	assert.Contains(t, err.Error(), "unsupported format [yaml]")
}

func TestParseFormat(t *testing.T) {
	tests := []struct {
		name    string
		format  string
		accept  string
		want    string
		wantErr string
	}{
		{name: "default", want: FormatJSON},
		{name: "explicit", format: "CSV", want: FormatCSV},
		{name: "accept txt", accept: "text/plain", want: FormatTXT},
		{name: "accept csv", accept: "text/csv; charset=utf-8", want: FormatCSV},
		{name: "accept tsv", accept: "text/tab-separated-values", want: FormatTSV},
		{name: "accept json", accept: "application/json", want: FormatJSON},
		{name: "format wins over accept", format: "tsv", accept: "text/plain", want: FormatTSV},
		{name: "unknown", format: "yaml", wantErr: "unsupported format [yaml]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			format, err := ParseFormat(tt.format, tt.accept)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, format)
		})
	}
}

func TestContentType(t *testing.T) {
	assert.Equal(t, "application/json; charset=utf-8", ContentType(FormatJSON))
	assert.Equal(t, "text/plain; charset=utf-8", ContentType(FormatTXT))
	assert.Equal(t, "text/csv; charset=utf-8", ContentType(FormatCSV))
	assert.Equal(t, "text/tab-separated-values; charset=utf-8", ContentType(FormatTSV))
}

func TestRenderValue(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{name: "nil", value: nil, want: "null"},
		{name: "string", value: "x", want: "x"},
		{name: "bool", value: true, want: "true"},
		{name: "float", value: 1.50, want: "1.5"},
		{name: "int64", value: int64(7), want: "7"},
		{name: "uint64", value: uint64(7), want: "7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderValue(tt.value))
		})
	}
}
