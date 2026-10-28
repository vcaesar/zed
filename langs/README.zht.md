<p align="center">
  <img src="../docs/logo_2/icon.svg" alt="ZedSearch" width="128" height="128">
</p>

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

![Search screen](../docs/hots/search-26.png)

## 索引畫面

![Index screen](../docs/hots/index-26.png)

# 入門

## 快速開始

請參閱 [快速開始](https://zincsearch-docs.zinc.dev/quickstart/)

# 發行版本

ZedSearch 已有數百個正式環境部署。

## 建置

設定 `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`，然後在儲存庫根目錄執行：

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` 會產生 `test/zedsearch-web`，一個內嵌 UI 的獨立二進位檔。

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
