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
	"github.com/zincsearch/zincsearch/pkg/zutils/json"
)

// ILMPhase is one phase of a lifecycle policy. Only min_age is interpreted;
// actions are stored as sent so Elasticsearch clients round-trip unchanged.
type ILMPhase struct {
	MinAge  string                 `json:"min_age,omitempty"`
	Actions map[string]interface{} `json:"actions,omitempty"`
}

// ILMPolicy is a minimal index lifecycle policy: the warm, cold and delete
// phases with the age at which an index enters them.
type ILMPolicy struct {
	Phases map[string]ILMPhase `json:"phases,omitempty"`
}

// ILMPolicyRecord is the stored form of a policy.
type ILMPolicyRecord struct {
	Name         string    `json:"name"`
	Version      int64     `json:"version"`
	ModifiedDate int64     `json:"modified_date"`
	Policy       ILMPolicy `json:"policy"`
}

type ilm struct{}

var ILM = new(ilm)

func (t *ilm) List(offset, limit int) ([]*ILMPolicyRecord, error) {
	data, err := db.List(t.key(""), offset, limit)
	if err != nil {
		return nil, err
	}
	policies := make([]*ILMPolicyRecord, 0, len(data))
	for _, d := range data {
		p := new(ILMPolicyRecord)
		if err := json.Unmarshal(d, p); err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, nil
}

func (t *ilm) Get(id string) (*ILMPolicyRecord, error) {
	data, err := db.Get(t.key(id))
	if err != nil {
		return nil, err
	}
	p := new(ILMPolicyRecord)
	err = json.Unmarshal(data, p)
	return p, err
}

func (t *ilm) Set(id string, val ILMPolicyRecord) error {
	val.Name = id
	data, err := json.Marshal(val)
	if err != nil {
		return err
	}
	return db.Set(t.key(id), data)
}

func (t *ilm) Delete(id string) error {
	return db.Delete(t.key(id))
}

func (t *ilm) key(id string) string {
	return "/ilm/" + id
}
