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

package index

import (
	"errors"
	"net/http"

	"github.com/vcaesar/zedsearch/pkg/config"
	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/meta"
	zedanalysis "github.com/vcaesar/zedsearch/pkg/uquery/analysis"
	"github.com/vcaesar/zedsearch/pkg/uquery/mappings"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// @Id CreateIndex
// @Summary Create index
// @security BasicAuth
// @Tags    Index
// @Accept  json
// @Produce json
// @Param   data body meta.IndexSimple true "Index data"
// @Success 200 {object} meta.HTTPResponseIndex
// @Failure 400 {object} meta.HTTPResponseError
// @Router /api/index [post]
func Create(c zhttp.Context) {
	var newIndex meta.IndexSimple
	if err := zutils.BindJSON(c, &newIndex); err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	indexName := c.Param("target")
	err := CreateIndexWorker(&newIndex, indexName)
	if err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	zutils.RenderJSON(c, http.StatusOK, meta.HTTPResponseIndex{
		Message:     "ok",
		Index:       newIndex.Name,
		StorageType: newIndex.StorageType,
	})
}

// @Id ESCreateIndex
// @Summary Create index for compatible ES
// @security BasicAuth
// @Tags    Index
// @Produce json
// @Param   index path  string  true  "Index"
// @Param   data  body  meta.IndexSimple true "Index data"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} meta.HTTPResponse
// @Router /es/{index} [put]
func CreateES(c zhttp.Context) {
	indexName := c.Param("target")

	var newIndex meta.IndexSimple
	if err := zutils.BindJSON(c, &newIndex); err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	// default the storage_type to disk, to provide the best possible integration
	newIndex.StorageType = core.StorageTypeDisk

	err := CreateIndexWorker(&newIndex, indexName)
	if err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}

	zutils.RenderJSON(c, http.StatusOK, zhttp.H{
		"acknowledged":        true,
		"shards_acknowledged": true,
		"index":               newIndex.Name,
	})
}

func CreateIndexWorker(newIndex *meta.IndexSimple, indexName string) error {
	if newIndex.StorageType == "" {
		newIndex.StorageType = core.StorageTypeDisk
	}
	if err := core.CheckStorageType(newIndex.StorageType); err != nil {
		return err
	}
	if newIndex.Name == "" && indexName != "" {
		newIndex.Name = indexName
	}

	if newIndex.Name == "" {
		return errors.New("index.name should be not empty")
	}

	if _, ok := core.GetIndex(newIndex.Name); ok {
		return errors.New("index [" + newIndex.Name + "] already exists")
	}

	if newIndex.Settings == nil {
		newIndex.Settings = new(meta.IndexSettings)
	}
	analyzers, err := zedanalysis.RequestAnalyzer(newIndex.Settings.Analysis)
	if err != nil {
		return errors.New(err.Error())
	}

	mappings, err := mappings.Request(analyzers, newIndex.Mappings)
	if err != nil {
		return errors.New(err.Error())
	}

	if newIndex.Settings != nil && newIndex.Settings.NumberOfShards != 0 {
		if newIndex.ShardNum == 0 {
			newIndex.ShardNum = newIndex.Settings.NumberOfShards
		}
	}
	if newIndex.ShardNum == 0 {
		newIndex.ShardNum = config.Global.Shard.Num
	}
	index, err := core.NewIndex(newIndex.Name, newIndex.StorageType, newIndex.ShardNum)
	if err != nil {
		return errors.New(err.Error())
	}

	// update settings
	if err := index.SetSettings(newIndex.Settings); err != nil {
		return err
	}

	// update analyzers
	if err := index.SetAnalyzers(analyzers); err != nil {
		return err
	}

	// update mappings
	if err := index.SetMappings(mappings); err != nil {
		return err
	}

	// store index
	if err = core.StoreIndex(index); err != nil {
		return errors.New(err.Error())
	}

	return nil
}
