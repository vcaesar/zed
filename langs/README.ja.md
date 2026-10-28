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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | 日本語 | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch は全文インデックスを行う検索エンジンです。Elasticsearch の軽量な代替であり、わずかなリソースで動作します。基盤となるインデックスライブラリとして [bluge](https://github.com/blugelabs/bluge)（[vcaesar/riot](https://github.com/vcaesar/riot) フォーク経由）を使用しています。

理解や調整に何十ものパラメータを必要とする Elasticsearch とは異なり、非常にシンプルで運用が簡単です。ZedSearch は 2 分で起動して動かすことができます。

API でデータを取り込み、Kibana で検索するだけであれば、Elasticsearch のドロップイン置き換えとして使えます（ZedSearch は Kibana をサポートしていません。ZedSearch は独自の UI を提供します）。

ZedSearch の簡単なデモは以下の動画をご覧ください。

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# なぜ ZedSearch なのか

Elasticsearch は非常に優れた製品ですが、複雑で多くのリソースを必要とし、10 年以上前から存在しています。私は、多くの作業をせずに誰もが全文検索インデックスを簡単に使えるように ZedSearch を作りました。

# 機能：

go + gin + react

1. 全文インデックス機能を提供
2. インストールと実行は単一バイナリで完結。複数プラットフォーム向けのバイナリを Releases で提供。
3. データをクエリするための React 製 Web UI（バイナリに埋め込み）
4. データ取り込み用の Elasticsearch 互換 API（単一レコードおよびバルク API）
5. すぐに使える認証機能
6. スキーマレス - 事前にスキーマを定義する必要がなく、同じインデックス内の異なるドキュメントが異なるフィールドを持てます。
7. インデックスはディスクに保存
8. 集計（aggregation）のサポート
9. イベント時刻の正規化とログ検索のデフォルト設定を備えた、オプトインの[ログインデックス](../pkg/logs/README.md)

# ドキュメント

ドキュメントは [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/) で参照できます。

# スクリーンショット

## 検索画面

![Search screen](../screenshots/search_screen.jpg)

## ユーザー管理画面

![Users screen](../screenshots/users_screen.jpg)

# はじめに

## クイックスタート

[クイックスタート](https://zincsearch-docs.zinc.dev/quickstart/)を参照してください

# リリース

ZedSearch は数百の本番環境で導入されています。

## ビルド

ソースビルドは**デフォルトで API のみ**です：Node.js、npm、`frontend/` ディレクトリは不要です。
初回起動時の認証情報を `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`
（または `conf/zed.toml`）で設定し、リポジトリのルートから実行します：

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zedsearch/: go run main.go [web]
```

`web` にはソースのチェックアウト、Go、Node.js 24+、npm が必要です。`npm ci`、
`npm run build`、`go build -tags web` を実行し、`test/zedsearch-web`
（Windows では `.exe`）を生成します。生成されたバイナリはすべての UI アセットを埋め込んでおり、
フロントエンドファイルや Node.js のない別のホストにコピーして使えます。起動時には呼び出し元の
作業ディレクトリ、設定、環境変数が維持されます。すでに `-tags web` でビルドされたバイナリは、
`web` 引数の有無にかかわらず、再ビルドせずに直接起動します。

手動ビルドの場合：

```sh
go build -o zedsearch ./cmd/zedsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zedsearch ./cmd/zedsearch
```

API のみのノードは `/` でバージョン JSON を返し、`/ui/` と Swagger UI は無効です。
Web ビルドは `/` を `/ui/` にリダイレクトし、設定で有効な場合は Swagger を提供します。
`sh/build.sh`、リリースバイナリ、標準の Docker イメージには引き続き UI が含まれます。

実験的なマルチノード・ドキュメントストアは、空の `cmd/distr/` ディレクトリではなく
`distribution/cmd` にあります。フロントエンドアセットは一切不要です：

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

ノードごとに異なる ID、データディレクトリ、Raft アドレス、gRPC アドレスを使用してください。
クラスターへの参加と TLS については [distribution/README.md](../distribution/README.md) を参照してください。これは
独立したレプリケーション型ドキュメントストアであり、分散型の ZedSearch REST/UI サーバーではありません。

- CI（lint、Linux/macOS/Windows での `go test`、カバレッジ）：[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml
- Nightly 開発イメージ `ghcr.io/vcaesar/zedsearch-dev:nightly`：[`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml
- リリース（`v*` タグ、goreleaser、`ghcr.io/vcaesar/zedsearch`）：[`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/release.yml

> **注意 — Nightly / 開発イメージ（push）はフォークでは失敗します：**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/vcaesar/zedsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> このワークフローはリポジトリの `GITHUB_TOKEN` で GHCR にログインしますが、このトークンはワークフローを実行している
> リポジトリのオーナー配下にしかパッケージを push できません。フォークではトークンが `vcaesar` 組織にアクセスできないため、
> push は拒否されます。`vcaesar/zedsearch` からワークフローを実行するか、`nightly.yml` の `env.IMAGE` を
> `ghcr.io/<your-owner>/zedsearch-dev` に変更してください（パッケージがリポジトリにリンクされていること、
> リポジトリに `packages: write` 権限があることを確認してください）。

# コミュニティ

- ZedSearch の開発とコントリビュートの方法

  [コントリビューションガイド](../CONTRIBUTING.md)を確認してください。[ロードマップ](https://zincsearch-docs.zinc.dev/roadmap/)もご覧ください

# 例

ZedSearch を使ってあらゆるデータをインデックス化・検索できます。以下は、zedsearch を使って Enron メールデータセットをインデックス化・検索した例です：

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
