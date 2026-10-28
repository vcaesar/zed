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
	"fmt"

	"github.com/vcaesar/riot"

	"github.com/vcaesar/zedsearch/pkg/config"
)

var ossBackend = &backend{newStore: func() (objectStore, error) { return newOSSStore(config.Global.OSS) }}

// GetOSSConfig returns a riot config that stores index items in the Alibaba
// Cloud OSS bucket from config.Global.OSS.
func GetOSSConfig(indexName string, timeRange ...int64) (riot.Config, error) {
	return getObjectConfig(ossBackend, indexName, timeRange)
}

// RemoveOSSIndex deletes every object stored for the index.
func RemoveOSSIndex(indexName string) error {
	return removeObjectIndex(ossBackend, indexName)
}

// newOSSStore talks to OSS through its S3-compatible API.
func newOSSStore(cfg config.OSSConfig) (*minioStore, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("oss storage is not configured: ZED_OSS_BUCKET is empty")
	}
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("oss storage is not configured: ZED_OSS_ENDPOINT is empty")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("oss storage requires ZED_OSS_ACCESS_KEY_ID and ZED_OSS_SECRET_ACCESS_KEY")
	}
	return newMinioStore(config.S3Config{
		Endpoint:        cfg.Endpoint,
		Bucket:          cfg.Bucket,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		UseSSL:          true,
	})
}
