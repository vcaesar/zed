/* Copyright 2022 Zinc Labs Inc. and Contributors
* Copyright 2026 AtomAI
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

package core

import (
	"errors"
	"os"
	"path"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/bluge/directory"
	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/metadata"
)

func DeleteIndex(name string) error {
	// 1. Check if index exists
	index, exists := GetIndex(name)
	if !exists {
		return errors.New("index " + name + " does not exists")
	}

	// 2. Close and Delete from cache
	ZED_INDEX_LIST.Delete(name)

	// 3. Physically delete the index.
	// The WAL always lives on local disk, so the data path is removed for every storage type.
	if err := os.RemoveAll(path.Join(config.Global.DataPath, index.GetName())); err != nil {
		log.Error().Err(err).Msg("failed to delete index")
	}
	switch index.GetStorageType() {
	case StorageTypeS3, StorageTypeMinIO:
		if err := directory.RemoveS3Index(index.GetName()); err != nil {
			log.Error().Err(err).Msg("failed to delete index from s3")
		}
	case StorageTypeGCS:
		if err := directory.RemoveGCSIndex(index.GetName()); err != nil {
			log.Error().Err(err).Msg("failed to delete index from gcs")
		}
	case StorageTypeOSS:
		if err := directory.RemoveOSSIndex(index.GetName()); err != nil {
			log.Error().Err(err).Msg("failed to delete index from oss")
		}
	}

	// 4. Delete form metadata
	return metadata.Index.Delete(name)
}
