[![CI](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml/badge.svg)](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/vcaesar/zedsearch.svg)](https://pkg.go.dev/github.com/vcaesar/zedsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/vcaesar/zedsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/vcaesar/zedsearch)](https://github.com/vcaesar/zedsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/vcaesar/zedsearch/branch/main/graph/badge.svg)](https://codecov.io/github/vcaesar/zedsearch)

[English](../README.md) | [简体中文](README.zh.md) | 繁體中文 | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch 是一個進行全文索引的搜尋引擎。它是 Elasticsearch 的輕量級替代方案，只需一小部分資源即可執行。它使用 [bluge](https://github.com/blugelabs/bluge)（透過 [vcaesar/riot](https://github.com/vcaesar/riot) 分支）作為底層索引函式庫。

相較於需要理解與調整數十個參數的 Elasticsearch，它非常簡單且容易維運。你可以在 2 分鐘內啟動並執行 ZedSearch。

如果你只是透過 API 寫入資料並使用 Kibana 進行搜尋，它可以直接取代 Elasticsearch（ZedSearch 不支援 Kibana，ZedSearch 提供自己的 UI）。

觀看下方影片，快速了解 ZedSearch 的展示。

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# 為什麼選擇 ZedSearch

雖然 Elasticsearch 是一款非常優秀的產品，但它複雜、需要大量資源，而且已有十多年歷史。我打造 ZedSearch 是為了讓大家不必做大量工作就能更輕鬆地使用全文搜尋索引。

# 功能：

go + gin + react

1. 提供全文索引能力
2. 單一執行檔即可安裝與執行。Releases 中提供多種平台的執行檔。
3. 以 React 撰寫的資料查詢 Web UI（內嵌於執行檔中）
4. 相容 Elasticsearch 的資料寫入 API（單筆記錄與批次 API）
5. 開箱即用的身分驗證
6. 無結構描述（Schema less）—— 無需預先定義 schema，同一索引中的不同文件可以擁有不同的欄位。
7. 索引儲存於磁碟
8. 支援聚合
9. 可選擇啟用的[日誌索引](../pkg/logs/README.md)，支援事件時間正規化與日誌搜尋預設設定

# 文件

文件位於 [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# 螢幕截圖

## 搜尋畫面

![Search screen](../screenshots/search_screen.jpg)

## 使用者管理畫面

![Users screen](../screenshots/users_screen.jpg)

# 入門

## 快速開始

請參閱 [快速開始](https://zincsearch-docs.zinc.dev/quickstart/)

# 發行版本

ZedSearch 已有數百個正式環境部署。

## 建置

原始碼建置**預設僅包含 API**：不需要 Node.js、npm 或 `frontend/` 目錄。
透過 `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`（或 `conf/zed.toml`）
設定首次啟動的憑證，然後在儲存庫根目錄執行：

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zedsearch/: go run main.go [web]
```

`web` 需要原始碼檢出、Go、Node.js 24+ 與 npm。它會執行 `npm ci`、
`npm run build` 與 `go build -tags web`，產生 `test/zedsearch-web`
（Windows 上為 `.exe`）。產生的執行檔內嵌所有 UI 資源，可以複製到
沒有前端檔案或 Node.js 的其他主機上執行。啟動時會保留呼叫者的工作目錄、
設定與環境變數。已使用 `-tags web` 建置的執行檔會直接啟動而不會重新建置，
無論是否帶有 `web` 參數。

手動建置：

```sh
go build -o zedsearch ./cmd/zedsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zedsearch ./cmd/zedsearch
```

僅 API 的節點在 `/` 回傳版本 JSON；`/ui/` 與 Swagger UI 會被停用。
Web 建置會將 `/` 重新導向到 `/ui/`，並在設定中啟用時提供 Swagger。
`sh/build.sh`、發行的執行檔與標準 Docker 映像檔仍然包含 UI。

實驗性的多節點文件儲存位於 `distribution/cmd`，而不是空的 `cmd/distr/` 目錄。
它從不需要前端資源：

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

每個節點需使用不同的 ID、資料目錄、Raft 位址與 gRPC 位址；
關於加入叢集與 TLS，請參閱 [distribution/README.md](../distribution/README.md)。這是一個
獨立的複寫文件儲存，而不是分散式的 ZedSearch REST/UI 伺服器。

- CI（lint、在 Linux/macOS/Windows 上執行 `go test`、覆蓋率）：[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml
- Nightly 開發映像檔 `ghcr.io/vcaesar/zedsearch-dev:nightly`：[`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml
- 發行（`v*` 標籤、goreleaser、`ghcr.io/vcaesar/zedsearch`）：[`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/release.yml

> **注意 —— Nightly / 開發映像檔（推送）在 fork 儲存庫中會失敗：**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/vcaesar/zedsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> 該工作流程使用儲存庫的 `GITHUB_TOKEN` 登入 GHCR，而該權杖只能向執行工作流程之儲存庫擁有者名下推送套件。
> 在 fork 中，該權杖無權存取 `vcaesar` 組織，因此推送被拒絕。請在 `vcaesar/zedsearch` 中執行該工作流程，
> 或者將 `nightly.yml` 中的 `env.IMAGE` 改為 `ghcr.io/<your-owner>/zedsearch-dev`（並確認該套件已連結到儲存庫 /
> 儲存庫擁有 `packages: write` 權限）。

# 社群

- 如何開發並為 ZedSearch 做出貢獻

  請參閱[貢獻指南](../CONTRIBUTING.md)。另請參閱[路線圖](https://zincsearch-docs.zinc.dev/roadmap/)

# 範例

你可以使用 ZedSearch 索引與搜尋任何資料。以下是一些社群成員使用 zedsearch 索引與搜尋 Enron 電子郵件資料集的範例：

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
