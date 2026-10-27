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
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// supported response formats.
const (
	FormatJSON = "json"
	FormatTXT  = "txt"
	FormatCSV  = "csv"
	FormatTSV  = "tsv"
)

// Response is the result of a SQL query, in the Elasticsearch SQL shape.
type Response struct {
	Columns []ResultColumn  `json:"columns"`
	Rows    [][]interface{} `json:"rows"`
}

// ResultColumn describes one output column.
type ResultColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// ParseFormat resolves the response format from the request format (body or
// query string) and the Accept header.
func ParseFormat(format, accept string) (string, error) {
	if format == "" {
		return acceptFormat(accept), nil
	}
	switch format := strings.ToLower(format); format {
	case FormatJSON, FormatTXT, FormatCSV, FormatTSV:
		return format, nil
	default:
		return "", sqlErrorf("unsupported format [%s]", format)
	}
}

func acceptFormat(accept string) string {
	switch {
	case strings.Contains(accept, "text/plain"):
		return FormatTXT
	case strings.Contains(accept, "text/csv"):
		return FormatCSV
	case strings.Contains(accept, "text/tab-separated-values"):
		return FormatTSV
	default:
		return FormatJSON
	}
}

// ContentType is the response content type of a format.
func ContentType(format string) string {
	switch format {
	case FormatTXT:
		return "text/plain; charset=utf-8"
	case FormatCSV:
		return "text/csv; charset=utf-8"
	case FormatTSV:
		return "text/tab-separated-values; charset=utf-8"
	default:
		return "application/json; charset=utf-8"
	}
}

// Render serializes the response in the given format.
func (r *Response) Render(format string) ([]byte, error) {
	switch format {
	case FormatTXT:
		return r.renderTXT(), nil
	case FormatCSV:
		return r.renderSeparated(',')
	case FormatTSV:
		return r.renderTSV(), nil
	case FormatJSON:
		return json.Marshal(r)
	default:
		return nil, sqlErrorf("unsupported format [%s]", format)
	}
}

// renderTXT draws the ascii table Elasticsearch returns for format=txt.
func (r *Response) renderTXT() []byte {
	widths := make([]int, len(r.Columns))
	for i, column := range r.Columns {
		widths[i] = len(column.Name)
	}
	cells := make([][]string, 0, len(r.Rows))
	for _, row := range r.Rows {
		line := make([]string, len(r.Columns))
		for i := range r.Columns {
			if i < len(row) {
				line[i] = renderValue(row[i])
			}
			if len(line[i]) > widths[i] {
				widths[i] = len(line[i])
			}
		}
		cells = append(cells, line)
	}

	var out bytes.Buffer
	headers := make([]string, len(r.Columns))
	separators := make([]string, len(r.Columns))
	for i, column := range r.Columns {
		headers[i] = center(column.Name, widths[i])
		separators[i] = strings.Repeat("-", widths[i])
	}
	out.WriteString(strings.Join(headers, "|") + "\n")
	out.WriteString(strings.Join(separators, "+") + "\n")
	for _, line := range cells {
		for i, cell := range line {
			line[i] = cell + strings.Repeat(" ", widths[i]-len(cell))
		}
		out.WriteString(strings.Join(line, "|") + "\n")
	}
	return out.Bytes()
}

func center(value string, width int) string {
	padding := width - len(value)
	if padding <= 0 {
		return value
	}
	left := padding / 2
	return strings.Repeat(" ", left) + value + strings.Repeat(" ", padding-left)
}

func (r *Response) renderSeparated(separator rune) ([]byte, error) {
	var out bytes.Buffer
	writer := csv.NewWriter(&out)
	writer.Comma = separator
	if err := writer.Write(r.headerCells()); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	for _, row := range r.Rows {
		if err := writer.Write(r.rowCells(row, "")); err != nil {
			return nil, fmt.Errorf("write row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("write rows: %w", err)
	}
	return out.Bytes(), nil
}

// renderTSV escapes tabs and newlines instead of quoting, like Elasticsearch.
func (r *Response) renderTSV() []byte {
	var out bytes.Buffer
	out.WriteString(strings.Join(r.headerCells(), "\t") + "\n")
	for _, row := range r.Rows {
		cells := r.rowCells(row, "")
		for i, cell := range cells {
			cells[i] = tsvEscape(cell)
		}
		out.WriteString(strings.Join(cells, "\t") + "\n")
	}
	return out.Bytes()
}

func tsvEscape(value string) string {
	value = strings.ReplaceAll(value, "\t", `\t`)
	value = strings.ReplaceAll(value, "\n", `\n`)
	return value
}

func (r *Response) headerCells() []string {
	cells := make([]string, 0, len(r.Columns))
	for _, column := range r.Columns {
		cells = append(cells, column.Name)
	}
	return cells
}

func (r *Response) rowCells(row []interface{}, null string) []string {
	cells := make([]string, len(r.Columns))
	for i := range r.Columns {
		if i >= len(row) || row[i] == nil {
			cells[i] = null
			continue
		}
		cells[i] = renderValue(row[i])
	}
	return cells
}

// renderValue is the textual form of a cell value.
func renderValue(value interface{}) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		return value
	case bool:
		return strconv.FormatBool(value)
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(value), 'f', -1, 32)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case uint64:
		return strconv.FormatUint(value, 10)
	default:
		return fmt.Sprintf("%v", value)
	}
}
