[![CI](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml/badge.svg)](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zincsearch/zincsearch.svg)](https://pkg.go.dev/github.com/zincsearch/zincsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zincsearch/zincsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/zincsearch/zincsearch)](https://github.com/zincsearch/zincsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/zincsearch/zincsearch/branch/main/graph/badge.svg)](https://codecov.io/github/zincsearch/zincsearch)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | 日本語 | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

❗注意：ユースケースがアプリ検索（アプリケーションや Web サイトに検索機能を実装すること）ではなくログ検索（アプリログやセキュリティログ）である場合は、ログ検索のユースケース専用に Rust で構築された [openobserve/openobserve](https://github.com/openobserve/openobserve) プロジェクトを検討してください。

# ZincSearch

ZincSearch は全文インデックスを行う検索エンジンです。Elasticsearch の軽量な代替であり、わずかなリソースで動作します。基盤となるインデックスライブラリとして [bluge](https://github.com/blugelabs/bluge)（[vcaesar/riot](https://github.com/vcaesar/riot) フォーク経由）を使用しています。

理解や調整に何十ものパラメータを必要とする Elasticsearch とは異なり、非常にシンプルで運用が簡単です。ZincSearch は 2 分で起動して動かすことができます。

API でデータを取り込み、Kibana で検索するだけであれば、Elasticsearch のドロップイン置き換えとして使えます（ZincSearch は Kibana をサポートしていません。ZincSearch は独自の UI を提供します）。

ZincSearch の簡単なデモは以下の動画をご覧ください。

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# なぜ ZincSearch なのか

Elasticsearch は非常に優れた製品ですが、複雑で多くのリソースを必要とし、10 年以上前から存在しています。私は、多くの作業をせずに誰もが全文検索インデックスを簡単に使えるように ZincSearch を作りました。

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

ZincSearch は数百の本番環境で導入されています。

## ビルド

ソースビルドは**デフォルトで API のみ**です：Node.js、npm、`frontend/` ディレクトリは不要です。
初回起動時の認証情報を `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
（または `conf/zinc.toml`）で設定し、リポジトリのルートから実行します：

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

`web` にはソースのチェックアウト、Go、Node.js 24+、npm が必要です。`npm ci`、
`npm run build`、`go build -tags web` を実行し、`test/zincsearch-web`
（Windows では `.exe`）を生成します。生成されたバイナリはすべての UI アセットを埋め込んでおり、
フロントエンドファイルや Node.js のない別のホストにコピーして使えます。起動時には呼び出し元の
作業ディレクトリ、設定、環境変数が維持されます。すでに `-tags web` でビルドされたバイナリは、
`web` 引数の有無にかかわらず、再ビルドせずに直接起動します。

手動ビルドの場合：

```sh
go build -o zincsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zincsearch ./cmd/zincsearch
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
独立したレプリケーション型ドキュメントストアであり、分散型の ZincSearch REST/UI サーバーではありません。

- CI（lint、Linux/macOS/Windows での `go test`、カバレッジ）：[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml
- Nightly 開発イメージ `ghcr.io/zincsearch/zincsearch-dev:nightly`：[`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml
- リリース（`v*` タグ、goreleaser、`ghcr.io/zincsearch/zincsearch`）：[`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/release.yml

> **注意 — Nightly / 開発イメージ（push）はフォークでは失敗します：**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/zincsearch/zincsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> このワークフローはリポジトリの `GITHUB_TOKEN` で GHCR にログインしますが、このトークンはワークフローを実行している
> リポジトリのオーナー配下にしかパッケージを push できません。フォークではトークンが `zincsearch` 組織にアクセスできないため、
> push は拒否されます。`zincsearch/zincsearch` からワークフローを実行するか、`nightly.yml` の `env.IMAGE` を
> `ghcr.io/<your-owner>/zincsearch-dev` に変更してください（パッケージがリポジトリにリンクされていること、
> リポジトリに `packages: write` 権限があることを確認してください）。

# ZincSearch と OpenObserve の比較

| 機能                 | ZincSearch                                                           | OpenObserve                                                                                      |
| -------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| 最適なユースケース   | アプリ検索                                                           | ログ、メトリクス、トレース（イミュータブルデータ）                                               |
| ストレージ           | ディスク                                                             | ディスク、オブジェクト（S3）、GCS、MinIO、swift など                                             |
| 推奨ユースケース     | アプリ検索                                                           | オブザーバビリティ（ログ、メトリクス、トレース）                                                 |
| 最大データ量         | 数百 GB                                                              | ペタバイト規模                                                                                   |
| 高可用性             | なし                                                                 | あり                                                                                             |
| オープンソース       | はい                                                                 | はい、[OpenObserve](https://github.com/openobserve/openobserve)                                  |
| ES API 互換性        | はい                                                                 | はい                                                                                             |
| GUI                  | 基本的                                                               | 非常に高度、ダッシュボードを含む                                                                 |
| コスト               | オープンソース                                                       | オープンソース                                                                                   |
| はじめに             | [オープンソースドキュメント](https://zincsearch-docs.zinc.dev/quickstart/) | [オープンソースドキュメント](https://openobserve.ai/docs) または [クラウド](https://cloud.openobserve.ai) |

# コミュニティ

- ZincSearch の開発とコントリビュートの方法

  [コントリビューションガイド](../CONTRIBUTING.md)を確認してください。[ロードマップ](https://zincsearch-docs.zinc.dev/roadmap/)もご覧ください

# 例

ZincSearch を使ってあらゆるデータをインデックス化・検索できます。以下は、zincsearch を使って Enron メールデータセットをインデックス化・検索した例です：

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
