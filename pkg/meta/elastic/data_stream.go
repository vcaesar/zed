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

package elastic

import (
	"net/http"

	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

func GetDataStream(c zhttp.Context) {
	target := c.Param("target")
	zutils.RenderJSON(c, http.StatusOK, zhttp.H{
		"data_streams": []zhttp.H{
			{
				"name": target,
				"timestamp_field": zhttp.H{
					"name": "@timestamp",
				},
			},
		},
	})
}

func PutDataStream(c zhttp.Context) {
	target := c.Param("target")
	zutils.RenderJSON(c, http.StatusOK, zhttp.H{
		"name":    target,
		"message": "ok",
	})
}
