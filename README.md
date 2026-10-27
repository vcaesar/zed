[![CI](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml/badge.svg)](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zincsearch/zincsearch.svg)](https://pkg.go.dev/github.com/zincsearch/zincsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zincsearch/zincsearch)](./go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/zincsearch/zincsearch)](https://github.com/zincsearch/zincsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/zincsearch/zincsearch/branch/main/graph/badge.svg)](https://codecov.io/github/zincsearch/zincsearch)

English | [简体中文](langs/README.zh.md) | [繁體中文](langs/README.zht.md) | [日本語](langs/README.ja.md) | [한국어](langs/README.ko.md) | [Français](langs/README.fr.md) | [Deutsch](langs/README.de.md) | [Español](langs/README.es.md) | [Русский](langs/README.ru.md) | [Português](langs/README.pt.md)

❗Note: If your use case is of log search (app and security logs) instead of app search (implement search feature in your application or website) then you should check [openobserve/openobserve](https://github.com/openobserve/openobserve) project built in rust that is specifically built for log search use case.

# ZincSearch

ZincSearch is a search engine that does full text indexing. It is a lightweight alternative to Elasticsearch and runs using a fraction of the resources. It uses [bluge](https://github.com/blugelabs/bluge) (via the [vcaesar/riot](https://github.com/vcaesar/riot) fork) as the underlying indexing library.

It is very simple and easy to operate as opposed to Elasticsearch which requires a couple dozen knobs to understand and tune. You can get ZincSearch up and running in 2 minutes.

It is a drop-in replacement for Elasticsearch if you are just ingesting data using APIs and searching using kibana (Kibana is not supported with ZincSearch. ZincSearch provides its own UI).

Check the below video for a quick demo of ZincSearch.

[![Zinc Youtube](./screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Why ZincSearch

While Elasticsearch is a very good product, it is complex and requires lots of resources and is more than a decade old. I built ZincSearch so it becomes easier for folks to use full text search indexing without doing a lot of work.

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

![Search screen](./screenshots/search_screen.jpg)

## User management screen

![Users screen](./screenshots/users_screen.jpg)

# Getting started

## Quickstart

Check [Quickstart](https://zincsearch-docs.zinc.dev/quickstart/)

# Releases

ZincSearch has hundreds of production installations.

## Build

Source builds are **API-only by default**: no Node.js, npm, or `frontend/` directory is needed.
Set first-start credentials via `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
(or `conf/zinc.toml`), then run from the repository root:

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

`web` requires the source checkout, Go, Node.js 24+ and npm. It runs `npm ci`,
`npm run build`, and `go build -tags web`, producing `test/zincsearch-web`
(`.exe` on Windows). The resulting binary embeds all UI assets and can be copied
to another host without frontend files or Node.js. Startup retains the caller's
working directory, configuration, and environment. A binary already built with
`-tags web` starts directly without rebuilding, with or without the `web` argument.

For manual builds:

```sh
go build -o zincsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zincsearch ./cmd/zincsearch
```

API-only nodes return version JSON at `/`; `/ui/` and the Swagger UI are disabled.
Web builds redirect `/` to `/ui/` and serve Swagger when enabled in configuration.
`sh/build.sh`, release binaries, and the standard Docker image still include the UI.

The experimental multi-node document store lives in `distribution/cmd`, not the
empty `cmd/distr/` directory. It never requires frontend assets:

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Use a distinct ID, data directory, Raft address and gRPC address for each node;
see [distribution/README.md](./distribution/README.md) for joins and TLS. This is
an independent replicated document store, not a distributed ZincSearch REST/UI server.

- CI (lint, `go test` on Linux/macOS/Windows, coverage): [`.github/workflows/ci.yml`](./.github/workflows/ci.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml
- Nightly dev image `ghcr.io/zincsearch/zincsearch-dev:nightly`: [`.github/workflows/nightly.yml`](./.github/workflows/nightly.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml
- Release (`v*` tags, goreleaser, `ghcr.io/zincsearch/zincsearch`): [`.github/workflows/release.yml`](./.github/workflows/release.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/release.yml

> **Note — Nightly / dev-image (push) fails on forks:**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/zincsearch/zincsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> The workflow logs in to GHCR with the repository's `GITHUB_TOKEN`, which can only push packages under the
> owner of the repository running the workflow. On a fork the token has no access to the `zincsearch` org, so
> the push is denied. Either run the workflow from `zincsearch/zincsearch`, or change `env.IMAGE` in
> `nightly.yml` to `ghcr.io/<your-owner>/zincsearch-dev` (and make sure the package is linked to the repo /
> the repo has `packages: write`).

# ZincSearch Vs OpenObserve

| Feature              | ZincSearch                                                       | OpenObserve                                                                              |
| -------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Ideal use case       | App search                                                       | Logs, metrics, traces (Immutable Data)                                                   |
| Storage              | Disk                                                             | Disk, Object (S3), GCS, MinIO, swift and more.                                           |
| Preferred Use case   | App search                                                       | Observability (Logs, metrics, traces)                                                    |
| Max data supported   | 100s of GBs                                                      | Petabyte scale                                                                           |
| High availability    | Not available                                                    | Yes                                                                                      |
| Open source          | Yes                                                              | Yes, [OpenObserve](https://github.com/openobserve/openobserve)                           |
| ES API compatibility | Yes                                                              | Yes                                                                                      |
| GUI                  | Basic                                                            | Very Advanced, including dashboards                                                      |
| Cost                 | Open source                                                      | Open source                                                                              |
| Get started          | [Open source docs](https://zincsearch-docs.zinc.dev/quickstart/) | [Open source docs](https://openobserve.ai/docs) or [Cloud](https://cloud.openobserve.ai) |

# Community

- How to develop and contribute to ZincSearch

  Check the [contributing guide](./CONTRIBUTING.md). Also check the [roadmap](https://zincsearch-docs.zinc.dev/roadmap/)

# Examples

You can use ZincSearch to index and search any data. Here are some examples that folks have created to index and search enron email dataset using zincsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
