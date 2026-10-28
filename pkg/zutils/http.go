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

package zutils

import (
	"io"
	"strings"

	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

func BindJSON(c zhttp.Context, obj interface{}) error {
	body, err := io.ReadAll(c.Body())
	if err != nil {
		return err
	}
	return json.Unmarshal(body, obj)
}

func RenderJSON(c zhttp.Context, code int, obj interface{}) {
	if requestsPrettyRendering(c) {
		c.IndentedJSON(code, obj)
	} else {
		c.JSON(code, obj)
	}
}

func requestsPrettyRendering(c zhttp.Context) bool {
	if p, ok := c.GetQuery("pretty"); ok && (strings.EqualFold(p, "true") || strings.EqualFold(p, "")) {
		return true
	}
	return false
}
