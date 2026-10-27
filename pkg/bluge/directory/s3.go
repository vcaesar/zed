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

package directory

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/vcaesar/riot"

	"github.com/zincsearch/zincsearch/pkg/config"
)

var s3Backend = &backend{newStore: func() (objectStore, error) { return newMinioStore(config.Global.S3) }}

// GetS3Config returns a riot config that stores index items as objects under
// "<indexName>/" in the bucket from config.Global.S3. Works with AWS S3, MinIO
// or any S3-compatible service.
func GetS3Config(indexName string, timeRange ...int64) (riot.Config, error) {
	return getObjectConfig(s3Backend, indexName, timeRange)
}

// RemoveS3Index deletes every object stored for the index.
func RemoveS3Index(indexName string) error {
	return removeObjectIndex(s3Backend, indexName)
}

// minioStore is the objectStore for every S3-compatible backend (S3, MinIO, GCS, OSS).
type minioStore struct {
	client *minio.Client
	bucket string
}

func newMinioStore(cfg config.S3Config) (*minioStore, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("s3 storage is not configured: ZINC_S3_BUCKET is empty")
	}
	var creds *credentials.Credentials
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		creds = credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	} else {
		creds = credentials.NewChainCredentials([]credentials.Provider{
			&credentials.EnvAWS{},
			&credentials.FileAWSCredentials{},
			&credentials.IAM{},
		})
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &minioStore{client: client, bucket: cfg.Bucket}, nil
}

func (s *minioStore) List(ctx context.Context, prefix string) ([]objectInfo, error) {
	var objs []objectInfo
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		objs = append(objs, objectInfo{Key: obj.Key, Size: obj.Size})
	}
	return objs, nil
}

func (s *minioStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}

func (s *minioStore) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{})
	return err
}

func (s *minioStore) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}
