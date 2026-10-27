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

	"github.com/zincsearch/zincsearch/pkg/config"
)

func TestNewMinioStore(t *testing.T) {
	_, err := newMinioStore(config.S3Config{})
	assert.Error(t, err, "bucket required")

	s, err := newMinioStore(config.S3Config{Endpoint: "localhost:9000", Bucket: "zinc", AccessKeyID: "a", SecretAccessKey: "b"})
	assert.NoError(t, err)
	assert.Equal(t, "zinc", s.bucket)

	s, err = newMinioStore(config.S3Config{Endpoint: "s3.amazonaws.com", Bucket: "zinc", UseSSL: true, Region: "us-east-1"})
	assert.NoError(t, err, "credential chain when keys are empty")
	assert.NotNil(t, s.client)

	_, err = newMinioStore(config.S3Config{Endpoint: "http://bad endpoint", Bucket: "zinc"})
	assert.Error(t, err)
}

func TestGetS3ConfigUnconfigured(t *testing.T) {
	// config.Global.S3.Bucket is empty in tests, so the shared store must fail to initialise.
	_, err := GetS3Config("idx")
	assert.Error(t, err)
	assert.Error(t, RemoveS3Index("idx"))
}
