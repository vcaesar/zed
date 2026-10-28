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

func TestNewOSSStore(t *testing.T) {
	_, err := newOSSStore(config.OSSConfig{})
	assert.Error(t, err, "bucket required")
	_, err = newOSSStore(config.OSSConfig{Bucket: "zed"})
	assert.Error(t, err, "endpoint required")
	_, err = newOSSStore(config.OSSConfig{Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "zed"})
	assert.Error(t, err, "keys required")

	s, err := newOSSStore(config.OSSConfig{Endpoint: "oss-cn-hangzhou.aliyuncs.com", Bucket: "zed", Region: "cn-hangzhou", AccessKeyID: "a", SecretAccessKey: "b"})
	assert.NoError(t, err)
	assert.Equal(t, "zed", s.bucket)
	assert.Equal(t, "https://oss-cn-hangzhou.aliyuncs.com", s.client.EndpointURL().String())
}

func TestGetOSSConfigUnconfigured(t *testing.T) {
	_, err := GetOSSConfig("idx")
	assert.Error(t, err)
	assert.Error(t, RemoveOSSIndex("idx"))
}
