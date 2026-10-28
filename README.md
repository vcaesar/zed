<p align="center">
  <img src="./docs/logo_2/icon.svg" alt="ZedSearch" width="128" height="128">
</p>

[![CI](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml/badge.svg)](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/vcaesar/zedsearch.svg)](https://pkg.go.dev/github.com/vcaesar/zedsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/vcaesar/zedsearch)](./go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/vcaesar/zedsearch)](https://github.com/vcaesar/zedsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/vcaesar/zedsearch/branch/main/graph/badge.svg)](https://codecov.io/github/vcaesar/zedsearch)

English | [简体中文](langs/README.zh.md) | [繁體中文](langs/README.zht.md) | [日本語](langs/README.ja.md) | [한국어](langs/README.ko.md) | [Français](langs/README.fr.md) | [Deutsch](langs/README.de.md) | [Español](langs/README.es.md) | [Русский](langs/README.ru.md) | [Português](langs/README.pt.md)

# ZedSearch

ZedSearch is a search engine that does full text indexing. It is a lightweight alternative to Elasticsearch and runs using a fraction of the resources. It uses [bluge](https://github.com/blugelabs/bluge) (via the [vcaesar/riot](https://github.com/vcaesar/riot) fork) as the underlying indexing library.

It is very simple and easy to operate as opposed to Elasticsearch which requires a couple dozen knobs to understand and tune. You can get ZedSearch up and running in 2 minutes.

It is a drop-in replacement for Elasticsearch if you are just ingesting data using APIs and searching using kibana (Kibana is not supported with ZedSearch. ZedSearch provides its own UI).

Check the below video for a quick demo of ZedSearch.

[![Zinc Youtube](./screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Why ZedSearch

While Elasticsearch is a very good product, it is complex and requires lots of resources and is more than a decade old. I built ZedSearch so it becomes easier for folks to use full text search indexing without doing a lot of work.

# Features:

go + gin + react

1. Provides full text indexing capability
2. Single binary for installation and running. Binaries available under releases for multiple platforms.
3. Web UI for querying data written in React (embedded in the binary)
4. Compatibility with Elasticsearch APIs for ingestion of data (single record and bulk API)
5. Out of the box authentication
6. Schema less - No need to define schema upfront and different documents in the same index can have different fields.
7. Index storage in disk
8. aggregation support
9. Opt-in [logs indexes](./pkg/logs/README.md) with event-time normalization and log search defaults

# Documentation

Documentation is available at [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# Screenshots

## Search screen

![Search screen](./docs/hots/search-26.png)

## Index screen

![Index screen](./docs/hots/index-26.png)

# Getting started

## Quickstart

Check [Quickstart](https://zincsearch-docs.zinc.dev/quickstart/)

# Releases

ZedSearch has hundreds of production installations.

## Build

Set `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` or configure `conf/zed.toml` (see below), then run from the repository root:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` produces `test/zedsearch-web`, a standalone binary with the UI embedded.

### Configuration

Settings are applied in order: built-in defaults ([`pkg/config/default.toml`](./pkg/config/default.toml)), then the optional `conf/zed.toml` in the working directory, then non-empty environment variables. Every key is also an environment variable in uppercase, e.g. `zed_server_port` → `ZED_SERVER_PORT`.

```toml
# conf/zed.toml
zed_first_admin_user = "admin"
zed_first_admin_password = "change-me"
zed_server_port = "4080"
zed_data_path = "./data"
zed_log_level = "info"

[Shard]
zed_shard_num = 3
```

- Only list the keys you change; grouped keys keep their table header from `default.toml` (`[Shard]`, `[S3]`, `[Etcd]`, ...).
- Unknown keys fail startup. Durations are in nanoseconds, sizes in bytes.
- The first admin credentials only create the admin user on first start; change the password after logging in and never commit real credentials.

# Community

- How to develop and contribute to ZedSearch

  Check the [contributing guide](./CONTRIBUTING.md). Also check the [roadmap](https://zincsearch-docs.zinc.dev/roadmap/)

# Examples

You can use ZedSearch to index and search any data. Here are some examples that folks have created to index and search enron email dataset using zedsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
