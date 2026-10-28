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

var gcsBackend = &backend{newStore: func() (objectStore, error) { return newGCSStore(config.Global.GCS) }}

// GetGCSConfig returns a riot config that stores index items in the Google
// Cloud Storage bucket from config.Global.GCS.
func GetGCSConfig(indexName string, timeRange ...int64) (riot.Config, error) {
	return getObjectConfig(gcsBackend, indexName, timeRange)
}

// RemoveGCSIndex deletes every object stored for the index.
func RemoveGCSIndex(indexName string) error {
	return removeObjectIndex(gcsBackend, indexName)
}

// newGCSStore talks to GCS through its S3-interoperable XML API, which
// authenticates with HMAC keys rather than service-account JSON.
func newGCSStore(cfg config.GCSConfig) (*minioStore, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("gcs storage is not configured: ZED_GCS_BUCKET is empty")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("gcs storage requires HMAC keys: ZED_GCS_ACCESS_KEY_ID and ZED_GCS_SECRET_ACCESS_KEY")
	}
	return newMinioStore(config.S3Config{
		Endpoint:        cfg.Endpoint,
		Bucket:          cfg.Bucket,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		UseSSL:          true,
	})
}
