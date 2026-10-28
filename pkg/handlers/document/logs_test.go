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

package document

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
	"github.com/vcaesar/zedsearch/test/utils"
)

func TestLogsBulkErrors(t *testing.T) {
	name := "TestLogsBulkErrors.index_1"
	index, err := core.NewIndex(name, "disk", 1)
	require.NoError(t, err)
	require.NoError(t, index.SetSettings(&meta.IndexSettings{IndexType: meta.IndexTypeLogs}))
	require.NoError(t, core.StoreIndex(index))
	t.Cleanup(func() { assert.NoError(t, core.DeleteIndex(name)) })

	body := `{"create":{"_index":"` + name + `","_id":"bad"}}
{"@timestamp":"invalid","message":"must not be acknowledged"}
{"index":{"_index":"` + name + `","_id":"good"}}
{"_timestamp":1704067201123456,"message":"valid event"}
`
	c, w := utils.NewGinContext()
	utils.SetGinRequestData(c, body)
	ESBulk(c)
	require.Equal(t, http.StatusOK, w.Code)
	var result BulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.True(t, result.Errors)
	require.Len(t, result.Items, 2)
	bad := result.Items[0]["create"]
	assert.Equal(t, http.StatusBadRequest, bad.Status)
	assert.Contains(t, bad.Error, "@timestamp")
	assert.Empty(t, bad.Result)
	assert.Equal(t, 0, bad.Shards.Successful)
	assert.Equal(t, 1, bad.Shards.Failed)
	assert.Equal(t, http.StatusOK, result.Items[1]["index"].Status)
	assert.Empty(t, result.Items[1]["index"].Error)
	require.Eventually(t, func() bool {
		_, err := index.GetDocument("good")
		return err == nil
	}, 10*time.Second, 20*time.Millisecond)
	_, err = index.GetDocument("bad")
	assert.Error(t, err)

	// The worker still returns an error for native bulk callers, with a full
	// per-item response for ES clients even when later documents succeed.
	resultPtr, err := BulkWorker("", strings.NewReader(body))
	assert.ErrorContains(t, err, "@timestamp")
	assert.True(t, resultPtr.Errors)
	assert.Len(t, resultPtr.Items, 2)
}

func TestESBulkMalformedMetadataResponse(t *testing.T) {
	c, w := utils.NewGinContext()
	utils.SetGinRequestData(c, `{"index":"not an object"}`+"\n")
	ESBulk(c)
	assert.Equal(t, http.StatusOK, w.Code)
	var result BulkResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	assert.True(t, result.Errors)
	assert.Contains(t, result.Error, "format error")
}
