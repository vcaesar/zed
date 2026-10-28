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

package config

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/pelletier/go-toml/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/vcaesar/ice/compress"
)

type config struct {
	FirstAdminUser            string        `toml:"zed_first_admin_user"`
	FirstAdminPassword        string        `toml:"zed_first_admin_password"`
	GinMode                   string        `toml:"gin_mode"`
	ServerPort                string        `toml:"zed_server_port"`
	ServerAddress             string        `toml:"zed_server_address"`
	ServerTLSCertificateFile  string        `toml:"zed_server_tls_certificate_file"`
	ServerTLSKeyFile          string        `toml:"zed_server_tls_key_file"`
	ServerMode                string        `toml:"zed_server_mode"`
	ServerBodyLimit           int           `toml:"zed_server_body_limit"` // max request body in bytes, fiber builds only
	NodeID                    int           `toml:"zed_node_id"`
	DataPath                  string        `toml:"zed_data_path"`
	MetadataStorage           string        `toml:"zed_metadata_storage"`
	IceCompressor             string        `toml:"zed_ice_compressor"`
	SentryEnable              bool          `toml:"zed_sentry"`
	SentryDSN                 string        `toml:"zed_sentry_dsn"`
	ProfilerEnable            bool          `toml:"zed_profiler"`
	ProfilerServer            string        `toml:"zed_profiler_server"`
	ProfilerAPIKey            string        `toml:"zed_profiler_api_key"`
	ProfilerFriendlyProfileID string        `toml:"zed_profiler_friendly_profile_id"`
	TelemetryEnable           bool          `toml:"zed_telemetry"`
	PrometheusEnable          bool          `toml:"zed_prometheus_enable"`
	EnableTextKeywordMapping  bool          `toml:"zed_enable_text_keyword_mapping"`
	EnableSQLMappings         bool          `toml:"zed_enable_sql_mappings"`
	BatchSize                 int           `toml:"zed_batch_size"`
	MaxResults                int           `toml:"zed_max_results"`
	AggregationTermsSize      int           `toml:"zed_aggregation_terms_size"`
	MaxDocumentSize           int           `toml:"zed_max_document_size"`   // Max size for a single document . Default = 1 MB = 1024 * 1024
	ObjectCacheSize           int64         `toml:"zed_object_cache_size"`   // local disk LRU cache for object storage indexes, bytes, 0 disables
	WalSyncInterval           time.Duration `toml:"zed_wal_sync_interval"`   // sync wal to disk, 1s, 10ms
	WalRedoLogNoSync          bool          `toml:"zed_wal_redolog_no_sync"` // control sync after every write
	ZedSwaggerEnable          bool          `toml:"zed_swagger_enable"`
	LogLevel                  string        `toml:"zed_log_level"`
	Cluster                   cluster
	Tier                      tier
	Shard                     shard
	Etcd                      etcd
	S3                        S3Config
	GCS                       GCSConfig
	OSS                       OSSConfig
	Plugin                    plugin
}

type cluster struct {
	Name string `toml:"zed_cluster_name"`
	// Enable turns on multi-node mode: membership, index ownership and
	// request routing are coordinated through etcd ([Etcd] section).
	Enable bool `toml:"zed_cluster_enable"`
	// NodeID must be unique and stable per node; defaults to hostname.
	NodeID string `toml:"zed_cluster_node_id"`
	// AdvertiseAddr is the http(s)://host:port other nodes use to reach this node.
	AdvertiseAddr string `toml:"zed_cluster_advertise_addr"`
	// Partitions is the fixed number of ownership buckets indexes hash into.
	Partitions        int           `toml:"zed_cluster_partitions"`
	HeartbeatInterval time.Duration `toml:"zed_cluster_heartbeat_interval"`
	HeartbeatTTL      time.Duration `toml:"zed_cluster_heartbeat_ttl"`
	// Replicas is the number of extra nodes that keep a warm read-only copy of
	// each partition's object-storage indexes for failover reads.
	Replicas int `toml:"zed_cluster_replicas"`
}

type tier struct {
	// Enable runs the background hot/warm/cold lifecycle mover.
	Enable        bool          `toml:"zed_tier_enable"`
	CheckInterval time.Duration `toml:"zed_tier_check_interval"`
	// ColdStorageType is where cold indexes are moved: s3, gcs or oss.
	ColdStorageType string `toml:"zed_tier_cold_storage_type"`
}

type shard struct {
	// control goroutine number for read
	GoroutineNum int `toml:"zed_shard_goroutine_num"`
	// DefaultNum is the default number of shards.
	Num int64 `toml:"zed_shard_num"`
	// MaxSize is the maximum size limit for one shard, or will create a new shard.
	MaxSize uint64 `toml:"zed_shard_max_size"`
}

type etcd struct {
	Endpoints []string `toml:"zed_etcd_endpoints"`
	Prefix    string   `toml:"zed_etcd_prefix"`
	Username  string   `toml:"zed_etcd_username"`
	Password  string   `toml:"zed_etcd_password"`
}

// S3Config configures the object store used by indexes created with storage_type "s3".
// Works with AWS S3, MinIO and any S3-compatible endpoint.
type S3Config struct {
	Endpoint        string `toml:"zed_s3_endpoint"`
	Bucket          string `toml:"zed_s3_bucket"`
	Region          string `toml:"zed_s3_region"`
	AccessKeyID     string `toml:"zed_s3_access_key_id"`
	SecretAccessKey string `toml:"zed_s3_secret_access_key"`
	UseSSL          bool   `toml:"zed_s3_use_ssl"`
}

// GCSConfig configures Google Cloud Storage used by indexes created with storage_type "gcs".
// Access goes through the S3-interoperable XML API of GCS, so HMAC keys are required.
type GCSConfig struct {
	Endpoint        string `toml:"zed_gcs_endpoint"`
	Bucket          string `toml:"zed_gcs_bucket"`
	AccessKeyID     string `toml:"zed_gcs_access_key_id"`
	SecretAccessKey string `toml:"zed_gcs_secret_access_key"`
}

// OSSConfig configures Alibaba Cloud OSS used by indexes created with storage_type "oss", via its S3-
// compatible API.
type OSSConfig struct {
	Endpoint        string `toml:"zed_oss_endpoint"`
	Bucket          string `toml:"zed_oss_bucket"`
	Region          string `toml:"zed_oss_region"`
	AccessKeyID     string `toml:"zed_oss_access_key_id"`
	SecretAccessKey string `toml:"zed_oss_secret_access_key"`
}

type plugin struct {
	ES  elasticsearch
	GSE gse
}

type elasticsearch struct {
	Version string `toml:"zed_plugin_es_version"`
}

type gse struct {
	Enable     bool   `toml:"zed_plugin_gse_enable"`
	EnableStop bool   `toml:"zed_plugin_gse_enable_stop"`
	EnableHMM  bool   `toml:"zed_plugin_gse_enable_hmm"`
	DictEmbed  string `toml:"zed_plugin_gse_dict_embed"`
	DictPath   string `toml:"zed_plugin_gse_dict_path"`
}

// configFile is the only config file read at startup, resolved against the working directory.
const configFile = "conf/zed.toml"

//go:embed default.toml
var defaultConfig []byte

var Global = new(config)

func init() {
	if err := load(Global); err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}

	// set the log level
	logLevel, logLevelErr := zerolog.ParseLevel(Global.LogLevel)
	if logLevelErr != nil {
		log.Error().Err(logLevelErr).Msg("ZED_LOG_LEVEL is not valid")
	}
	zerolog.SetGlobalLevel(logLevel)

	// check data path
	testPath := path.Join(Global.DataPath, "_test_")
	if err := os.MkdirAll(testPath, 0o755); err != nil {
		log.Fatal().Err(err).Msg("ZED_DATA_PATH is not writable")
	}
	if err := os.Remove(testPath); err != nil {
		log.Fatal().Err(err).Msg("ZED_DATA_PATH is not writable")
	}

	// configure ice compress algorithm
	switch strings.ToUpper(Global.IceCompressor) {
	case "SNAPPY":
		compress.Algorithm = compress.SNAPPY
	case "S2":
		compress.Algorithm = compress.S2
	case "ZSTD":
		compress.Algorithm = compress.ZSTD
	}
}

// load fills c from the embedded defaults, then ./conf/zed.toml when present,
// then non-empty process environment variables. TOML keys are lowercase and
// map to the same names uppercased in the environment.
func load(c *config) error {
	if err := toml.Unmarshal(defaultConfig, c); err != nil {
		return fmt.Errorf("decode built-in defaults: %w", err)
	}
	data, err := os.ReadFile(configFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read config %s: %w", configFile, err)
	}
	if err == nil {
		if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(c); err != nil {
			// Decoder errors can contain configuration values, including passwords.
			return fmt.Errorf("invalid TOML configuration in %s", configFile)
		}
	}
	loadEnv(reflect.ValueOf(c).Elem())
	return nil
}

func loadEnv(rv reflect.Value) {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		fv := rv.Field(i)
		ft := rt.Field(i)
		if ft.Type.Kind() == reflect.Struct {
			loadEnv(fv)
			continue
		}
		if tag := ft.Tag.Get("toml"); tag != "" {
			setField(fv, strings.ToUpper(tag))
		}
	}
}

func setField(field reflect.Value, tag string) {
	if tag == "" {
		return
	}
	v := os.Getenv(tag)
	if v == "" {
		return
	}
	switch field.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		_, ok := field.Interface().(time.Duration)
		var (
			vi  int64
			err error
		)
		switch ok {
		case true:
			d, e := time.ParseDuration(v)
			if e != nil && strings.Contains(e.Error(), "time: missing unit in duration") {
				vi, err = strconv.ParseInt(v, 10, 64)
			} else {
				vi, err = int64(d), e
			}

		default:
			vi, err = units.FromHumanSize(v)
		}
		if err != nil {
			log.Fatal().Err(err).Msgf("env %s is not int", tag)
		}

		field.SetInt(int64(vi))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		vi, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			log.Fatal().Err(err).Msgf("env %s is not uint", tag)
		}
		field.SetUint(uint64(vi))
	case reflect.Bool:
		vi, err := strconv.ParseBool(v)
		if err != nil {
			log.Fatal().Err(err).Msgf("env %s is not bool", tag)
		}
		field.SetBool(vi)
	case reflect.String:
		field.SetString(v)
	case reflect.Slice:
		vs := strings.Split(v, ",")
		field.Set(reflect.ValueOf(vs))
		field.SetLen(len(vs))
	}
}
