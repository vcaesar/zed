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

package directory

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vcaesar/zedsearch/pkg/config"
)

func TestNewGCSStore(t *testing.T) {
	_, err := newGCSStore(config.GCSConfig{})
	assert.Error(t, err, "bucket required")
	_, err = newGCSStore(config.GCSConfig{Endpoint: "storage.googleapis.com", Bucket: "zinc"})
	assert.Error(t, err, "HMAC keys required")
	_, err = newGCSStore(config.GCSConfig{Endpoint: "storage.googleapis.com", Bucket: "zinc", AccessKeyID: "a"})
	assert.Error(t, err, "both keys required")

	s, err := newGCSStore(config.GCSConfig{Endpoint: "storage.googleapis.com", Bucket: "zinc", AccessKeyID: "a", SecretAccessKey: "b"})
	assert.NoError(t, err)
	assert.Equal(t, "zinc", s.bucket)
	assert.Equal(t, "https://storage.googleapis.com", s.client.EndpointURL().String())

	_, err = newGCSStore(config.GCSConfig{Endpoint: "other.googleapis.com", Bucket: "zinc", AccessKeyID: "a", SecretAccessKey: "b"})
	assert.Error(t, err, "minio rejects non-storage googleapis hosts")
}

func TestGetGCSConfigUnconfigured(t *testing.T) {
	_, err := GetGCSConfig("idx")
	assert.Error(t, err)
	assert.Error(t, RemoveGCSIndex("idx"))
}
