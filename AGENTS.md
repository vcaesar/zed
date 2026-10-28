# AGENTS.md

ZedSearch — lightweight Elasticsearch alternative. Go 1.25 server (`github.com/vcaesar/zedsearch`) with an embedded React/TypeScript/Vite console.

## Layout

- `cmd/zedsearch/main.go` — entrypoint (telemetry, HTTP server, shutdown); swag annotation root.
- `embed.go` — `//go:embed frontend/dist` behind the `web` build tag; default builds are API-only. `pkg/webbuild` handles the source CLI's opt-in `web` rebuild.
- `pkg/config` — `config.Global`: embedded `default.toml` → optional `./conf/zed.toml` → env vars. Field tag `toml:"zed_server_port"` is both the TOML key and, uppercased, the env var. Durations in ns, sizes in bytes. No `.env`, no `ZED_CONFIG_FILE`.
- `pkg/zhttp` — HTTP layer: `zhttp.Context`/`Engine` with gin middleware semantics; gin by default (`gin.go`), fiber v3 with `-tags fiber` (`fiber.go`). `NewHTTPContext` backs handler tests.
- `pkg/routes` — `/api/...` (native) and `/es/...` (Elasticsearch-compatible), auth middleware, pprof (debug only), swagger.
- `pkg/handlers/{auth,index,document,search}` — `zhttp.Context` handlers.
- `pkg/core` — indexes, shards, WAL-backed writes, search.
- `pkg/bluge`, `pkg/uquery` — Bluge wrappers, query DSL / aggregations.
- `pkg/metadata` — persistence: bolt (default), badger, etcd.
- `pkg/meta`, `pkg/errors`, `pkg/zutils`, `pkg/wal`, `pkg/ider`, `pkg/upgrade` — types, helpers, WAL, ids, on-disk upgrades.
- `docker/` — Dockerfiles (`.ci` nightly, `.release` goreleaser); `sh/` — scripts, runnable from anywhere.
- `docs/` — generated swagger; `test/api`, `test/benchmark` — HTTP and benchmark tests; `web/` — legacy Vue UI, not built.

## Build / run

Node.js 24 + npm; keep `frontend/package-lock.json` in sync.

```shell
go build -o zedsearch cmd/zedsearch/main.go      # API-only, no frontend needed
./sh/build.sh                                     # frontend + versioned web binary
ZED_FIRST_ADMIN_USER=admin ZED_FIRST_ADMIN_PASSWORD=... go run cmd/zedsearch/main.go  # API-only :4080
go run cmd/zedsearch/main.go web                  # npm ci/build + tagged rebuild + start
cd frontend && npm ci && npm run build && cd ..   # only needed for -tags web builds/tests
go build -tags web -o zedsearch cmd/zedsearch/main.go
go build -tags fiber -o zedsearch cmd/zedsearch/main.go  # fiber instead of gin (combine: -tags web,fiber)
cd frontend && npm run dev                        # UI dev server
./sh/develop.sh                                   # reflex live-reload
```

Version via `-ldflags -X .../pkg/meta.{Version,CommitHash,BuildDate}`.

## Test / lint

```shell
./sh/test.sh [TestFoo|bench]  # go test -v ./... (sets env, cleans data dirs); arg → -test.run, or benchmarks
./sh/coverage.sh              # -race -coverpkg=./...; fails under 70%
./sh/golangci-lint.sh         # golangci-lint v2, .golangci.yaml
./sh/swagger.sh               # regenerate docs/ after swag annotation changes
cd frontend && npm run build  # TS check + assets
```

CI (GitHub Actions): `ci.yml` — lint, vet, build, test on ubuntu/macos/windows + coverage to Codecov; `nightly.yml` — `ghcr.io/vcaesar/zedsearch-dev:nightly`; `release.yml` — goreleaser on `v*` tags (`.goreleaser.yml`).

## Conventions

- Apache-2.0 "Copyright 2026 AtomAI. and Contributors" header on every Go file.
- Imports: stdlib, third-party, `github.com/vcaesar/zedsearch/...`.
- `pkg/zutils/json` instead of `encoding/json`; logging via `github.com/rs/zerolog/log`.
- New settings: field in `pkg/config` struct with `toml:"zed_..."` tag + default in `default.toml`.
- Tests: `*_test.go` beside code, `testify/assert`, table-driven with `t.Run`. Name index fixtures after the test (`TestIndex_Index.index_1`) — indexes are global and collide.
- Handlers take `zhttp.Context`, never fiber or gin types; only `pkg/zhttp` imports them. Handler tests use `test/utils.NewContext`.
- `test/api` tests share the `zhttp.Engine` from `init.go` (`server()`, `request(method, path, body)`, basic auth `admin`).

## Pitfalls

- Only `-tags web` builds/tests require `frontend/dist`; rebuild assets after frontend changes. Plain Go builds/tests must work without `frontend/`. The `web` command requires a source checkout and writes `test/zedsearch-web`; concurrent node launches serialize on `test/zedsearch-web.lock`.
- Tests create `data/` under `pkg/` and `test/`; `sh/test.sh` cleans them, plain `go test` does not.
- First run needs `ZED_FIRST_ADMIN_USER`/`ZED_FIRST_ADMIN_PASSWORD`; never commit a `./conf/zed.toml` with real credentials.
- `ZED_METADATA_STORAGE=etcd` tests need a running etcd with auth (`test/README.md`).
- Behavior changes usually need both `/api` and `/es` routes updated.
- Only `-tags fiber` builds cap request bodies at `zed_server_body_limit` (100 MB); default gin builds are unlimited.
- Coverage gate is 70% — add tests with behavior changes.
