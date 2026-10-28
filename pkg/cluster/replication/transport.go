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
	"archive/tar"
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/vcaesar/zedsearch/pkg/cluster"
	"github.com/vcaesar/zedsearch/pkg/zutils/json"
)

// Node to node endpoints, authenticated with SecretHeader.
const (
	ReplicatePath = "/api/_internal/replication/replicate"
	RecoverPath   = "/api/_internal/replication/recover"
	SnapshotPath  = "/api/_internal/replication/snapshot"
	SecretHeader  = "X-Zed-Cluster-Secret"

	// snapshotMetaName is the first tar entry of a snapshot.
	snapshotMetaName = "_snapshot.json"
)

// ErrNotModified is returned by Snapshot when the replica is current.
var ErrNotModified = errors.New("replication: snapshot not modified")

// Op is one WAL entry recorded on the owner.
type Op struct {
	Seq   uint64          `json:"seq"`
	Shard string          `json:"shard"`
	Entry json.RawMessage `json:"entry"`
}

// Batch carries the operations From.. of one index to a replica.
type Batch struct {
	Index    string          `json:"index"`
	Term     string          `json:"term"`
	From     uint64          `json:"from"`
	Ops      []Op            `json:"ops"`
	Mappings json.RawMessage `json:"mappings,omitempty"`
}

// Ack is a replica's answer: the term it follows and its last operation.
type Ack struct {
	Term       string `json:"term"`
	Seq        uint64 `json:"seq"`
	Recovering bool   `json:"recovering,omitempty"`
}

// Checkpoint names a point of an owner's history.
type Checkpoint struct {
	Term string `json:"term"`
	Seq  uint64 `json:"seq"`
}

// SnapshotInfo describes a file copy: the history point it contains at
// least and the index metadata matching the files.
type SnapshotInfo struct {
	Checkpoint
	Index json.RawMessage `json:"index"`
}

// Transport is how replication reaches other nodes.
type Transport interface {
	// Replicate sends a batch; ErrOutOfSync comes with the replica's Ack.
	Replicate(ctx context.Context, node string, batch *Batch) (Ack, error)
	// Recover asks a replica to copy the index from its owner.
	Recover(ctx context.Context, node, index string) error
	// Snapshot copies the files of index from owner into dir, unless the
	// owner is still at have (ErrNotModified).
	Snapshot(ctx context.Context, owner, index string, have Checkpoint, dir string) (*SnapshotInfo, error)
}

// HTTPTransport calls the internal endpoints of the other nodes.
type HTTPTransport struct {
	secret string
	client *http.Client
}

// NewHTTPTransport builds a transport authenticating with secret.
func NewHTTPTransport(secret string) *HTTPTransport {
	return &HTTPTransport{secret: secret, client: &http.Client{}}
}

// CheckSecret reports whether a request carries the cluster secret.
func CheckSecret(header, secret string) bool {
	return secret != "" && subtle.ConstantTimeCompare([]byte(header), []byte(secret)) == 1
}

func (t *HTTPTransport) do(ctx context.Context, method, node, path string, body []byte) (*http.Response, error) {
	addr, ok := cluster.NodeAddr(node)
	if !ok {
		return nil, fmt.Errorf("replication: node %s is unreachable", node)
	}
	req, err := http.NewRequestWithContext(ctx, method, addr+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SecretHeader, t.secret)
	return t.client.Do(req)
}

func closeBody(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		log.Warn().Err(err).Msg("replication: close response body")
	}
}

// Replicate implements Transport.
func (t *HTTPTransport) Replicate(ctx context.Context, node string, batch *Batch) (Ack, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payload, err := json.Marshal(batch)
	if err != nil {
		return Ack{}, err
	}
	resp, err := t.do(ctx, http.MethodPost, node, ReplicatePath, payload)
	if err != nil {
		return Ack{}, err
	}
	defer closeBody(resp)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Ack{}, err
	}
	var ack Ack
	switch resp.StatusCode {
	case http.StatusOK, http.StatusConflict:
		if err := json.Unmarshal(data, &ack); err != nil {
			return Ack{}, err
		}
		if resp.StatusCode == http.StatusConflict {
			return ack, ErrOutOfSync
		}
		return ack, nil
	default:
		return Ack{}, fmt.Errorf("replication: %s answered %s: %s", node, resp.Status, data)
	}
}

// Recover implements Transport.
func (t *HTTPTransport) Recover(ctx context.Context, node, index string) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	resp, err := t.do(ctx, http.MethodPost, node, RecoverPath+"?index="+url.QueryEscape(index), nil)
	if err != nil {
		return err
	}
	defer closeBody(resp)
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body) // best effort, only for the message
		return fmt.Errorf("replication: %s answered %s: %s", node, resp.Status, data)
	}
	return nil
}

// Snapshot implements Transport.
func (t *HTTPTransport) Snapshot(ctx context.Context, owner, index string, have Checkpoint, dir string) (*SnapshotInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	query := url.Values{"index": {index}, "term": {have.Term}, "seq": {strconv.FormatUint(have.Seq, 10)}}
	resp, err := t.do(ctx, http.MethodGet, owner, SnapshotPath+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer closeBody(resp)
	switch resp.StatusCode {
	case http.StatusOK:
		return ReadSnapshot(resp.Body, dir)
	case http.StatusNotModified:
		return nil, ErrNotModified
	default:
		data, _ := io.ReadAll(resp.Body) // best effort, only for the message
		return nil, fmt.Errorf("replication: %s answered %s: %s", owner, resp.Status, data)
	}
}

// WriteSnapshot streams info and every file under dir as a tar archive.
func WriteSnapshot(w io.Writer, info *SnapshotInfo, dir string) error {
	tw := tar.NewWriter(w)
	header, err := json.Marshal(info)
	if err != nil {
		return err
	}
	if err := writeTarFile(tw, snapshotMetaName, int64(len(header)), bytes.NewReader(header)); err != nil {
		return err
	}
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		stat, err := f.Stat()
		if err == nil {
			err = writeTarFile(tw, filepath.ToSlash(rel), stat.Size(), f)
		}
		return errors.Join(err, f.Close())
	})
	if err != nil {
		return err
	}
	return tw.Close()
}

func writeTarFile(tw *tar.Writer, name string, size int64, r io.Reader) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o640, Size: size, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := io.Copy(tw, r)
	return err
}

// ReadSnapshot unpacks a WriteSnapshot archive into dir.
func ReadSnapshot(r io.Reader, dir string) (*SnapshotInfo, error) {
	tr := tar.NewReader(r)
	var info *SnapshotInfo
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Name == snapshotMetaName {
			info = new(SnapshotInfo)
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(data, info); err != nil {
				return nil, err
			}
			continue
		}
		target := filepath.Join(dir, filepath.FromSlash(header.Name))
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return nil, fmt.Errorf("replication: snapshot entry %q escapes the target", header.Name)
		}
		if err := writeFile(target, tr); err != nil {
			return nil, err
		}
	}
	if info == nil {
		return nil, errors.New("replication: snapshot without metadata")
	}
	return info, nil
}

func writeFile(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, r)
	return errors.Join(err, f.Close())
}
