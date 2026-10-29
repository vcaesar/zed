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

package replication

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/core"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// stateFile keeps the replica position next to the index data, so it goes
// away with the data when a copy is replaced or dropped.
const stateFile = "_replica.json"

// replicaState is the persisted position of a local copy. It may lag the
// data on disk, never lead it: replayed operations are idempotent, missing
// ones are not.
type replicaState struct {
	Mode     string `json:"mode"`
	Term     string `json:"term"`
	Seq      uint64 `json:"seq"`
	FilesSeq uint64 `json:"files_seq"`
}

func statePath(name string) string {
	return filepath.Join(indexDir(name), stateFile)
}

// loadState reads the position of the local copy of a disk index recorded
// in mode, false when there is none to resume from.
func loadState(name, mode string) (replicaState, bool) {
	index, ok := core.GetIndex(name)
	if !ok || !index.IsDisk() {
		return replicaState{}, false
	}
	data, err := os.ReadFile(statePath(name))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn().Err(err).Str("index", name).Msg("replication: read replica state")
		}
		return replicaState{}, false
	}
	var st replicaState
	if err := json.Unmarshal(data, &st); err != nil {
		log.Warn().Err(err).Str("index", name).Msg("replication: parse replica state")
		return replicaState{}, false
	}
	return st, st.Mode == mode && st.Term != ""
}

// saveState writes st durably, replacing the previous record atomically.
func saveState(name string, st replicaState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := statePath(name)
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if err = errors.Join(err, f.Close()); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// removeState forgets the position: the local copy now takes local writes.
func removeState(name string) error {
	if err := os.Remove(statePath(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// persist records the position of a ready copy when it moved. A document
// replica syncs its WAL first, so the record never claims operations the
// disk may lose. The caller holds r.mu.
func (r *replica) persist(name, mode string) error {
	if !r.ready {
		return nil
	}
	st := replicaState{Mode: mode, Term: r.term, Seq: r.filesSeq, FilesSeq: r.filesSeq}
	if mode == ModeDocument {
		st.Seq = r.seq
	}
	if st == r.saved {
		return nil
	}
	index, ok := core.GetIndex(name)
	if !ok {
		return nil
	}
	if _, err := os.Stat(indexDir(name)); errors.Is(err, fs.ErrNotExist) {
		return nil // no local data, a restart has nothing to resume
	}
	if mode == ModeDocument {
		if err := index.SyncWAL(); err != nil {
			return err
		}
	}
	if err := saveState(name, st); err != nil {
		return err
	}
	r.saved = st
	return nil
}
