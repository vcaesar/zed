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

package metadata

import (
	"errors"
	"strings"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/metadata/storage"
	"github.com/vcaesar/zedsearch/pkg/metadata/storage/badger"
	"github.com/vcaesar/zedsearch/pkg/metadata/storage/bolt"
	"github.com/vcaesar/zedsearch/pkg/metadata/storage/etcd"
)

var ErrorKeyNotExists = errors.New("key not exists")

var db storage.Storager

func init() {
	switch backend(config.Global.MetadataStorage, config.Global.ServerMode, config.Global.Cluster.Enable) {
	case "etcd":
		db = etcd.New(config.Global.Etcd.Prefix + "/metadata")
	case "badger":
		db = badger.New("_metadata.db")
	default:
		db = bolt.New("_metadata.bolt")
	}
}

// backend picks the metadata store. Cluster nodes must share index metadata,
// so cluster mode always uses etcd whatever zed_metadata_storage says.
func backend(metadataStorage, serverMode string, clusterEnable bool) string {
	if clusterEnable || strings.EqualFold(serverMode, "cluster") {
		return "etcd"
	}
	switch storage := strings.ToLower(metadataStorage); storage {
	case "etcd", "badger":
		return storage
	default:
		return "bolt"
	}
}

func Close() error {
	return db.Close()
}
