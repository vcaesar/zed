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

package cluster

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/cluster/replication"
	"github.com/vcaesar/zedsearch/pkg/meta"
	"github.com/vcaesar/zedsearch/pkg/zhttp"
	"github.com/vcaesar/zedsearch/pkg/zutils"
)

// ReplicationReplicate applies a batch of operations shipped by the owner.
// 409 carries the replica's position when it cannot apply the batch.
func ReplicationReplicate(c zhttp.Context) {
	batch := new(replication.Batch)
	if err := zutils.BindJSON(c, batch); err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	ack, err := replication.Apply(batch)
	switch {
	case err == nil:
		zutils.RenderJSON(c, http.StatusOK, ack)
	case errors.Is(err, replication.ErrOutOfSync):
		zutils.RenderJSON(c, http.StatusConflict, ack)
	default:
		log.Error().Err(err).Str("index", batch.Index).Msg("replication: apply batch")
		zutils.RenderJSON(c, http.StatusInternalServerError, meta.HTTPResponseError{Error: err.Error()})
	}
}

// ReplicationRecover makes this replica copy an index from its owner.
func ReplicationRecover(c zhttp.Context) {
	if err := replication.RequestRecovery(c.Query("index")); err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	zutils.RenderJSON(c, http.StatusOK, zhttp.H{"recovering": true})
}

// ReplicationSnapshot streams a file copy of an owned index as a tar. POST
// carries a replication.SnapshotRequest, GET (older nodes) the checkpoint
// in the query and no held files.
func ReplicationSnapshot(c zhttp.Context) {
	req, err := snapshotRequest(c)
	if err != nil {
		zutils.RenderJSON(c, http.StatusBadRequest, meta.HTTPResponseError{Error: err.Error()})
		return
	}
	index := req.Index
	c.ServeHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := false
		err := replication.ServeSnapshot(r.Context(), req, func() io.Writer {
			started = true
			w.Header().Set("Content-Type", "application/x-tar")
			w.WriteHeader(http.StatusOK)
			return w
		})
		switch {
		case err == nil:
		case errors.Is(err, replication.ErrNotModified):
			w.WriteHeader(http.StatusNotModified)
		case started:
			// the status is out, a broken tar makes the replica fail
			log.Error().Err(err).Str("index", index).Msg("replication: stream snapshot")
		default:
			log.Error().Err(err).Str("index", index).Msg("replication: snapshot")
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}
	}))
}

func snapshotRequest(c zhttp.Context) (*replication.SnapshotRequest, error) {
	req := new(replication.SnapshotRequest)
	if c.Method() == http.MethodPost {
		return req, zutils.BindJSON(c, req)
	}
	seq, err := strconv.ParseUint(c.DefaultQuery("seq", "0"), 10, 64)
	if err != nil {
		return nil, err
	}
	req.Index = c.Query("index")
	req.Have = replication.Checkpoint{Term: c.Query("term"), Seq: seq}
	return req, nil
}
