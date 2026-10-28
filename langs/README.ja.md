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

![Search screen](../docs/hots/search-26.png)

## インデックス画面

![Index screen](../docs/hots/index-26.png)

# はじめに

## クイックスタート

[クイックスタート](https://zincsearch-docs.zinc.dev/quickstart/)を参照してください

# リリース

ZedSearch は数百の本番環境で導入されています。

## ビルド

`ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` を設定し、リポジトリのルートで実行します：

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` は UI を組み込んだ単体バイナリ `test/zedsearch-web` を生成します。

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
