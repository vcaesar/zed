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

[English](../README.md) | 简体中文 | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch 是一个进行全文索引的搜索引擎。它是 Elasticsearch 的轻量级替代品，只需一小部分资源即可运行。它使用 [bluge](https://github.com/blugelabs/bluge)（通过 [vcaesar/riot](https://github.com/vcaesar/riot) 分支）作为底层索引库。

与需要理解和调整几十个参数的 Elasticsearch 相比，它非常简单且易于运维。你可以在 2 分钟内启动并运行 ZedSearch。

如果你只是通过 API 写入数据并使用 Kibana 进行搜索，它可以直接替代 Elasticsearch（ZedSearch 不支持 Kibana，ZedSearch 提供了自己的 UI）。

观看下面的视频，快速了解 ZedSearch 的演示。

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# 为什么选择 ZedSearch

虽然 Elasticsearch 是一款非常优秀的产品，但它复杂、需要大量资源，而且已有十多年历史。我构建 ZedSearch 是为了让大家无需做大量工作就能更轻松地使用全文搜索索引。

# 特性：

go + gin + react

1. 提供全文索引能力
2. 单个二进制文件即可安装和运行。Releases 中提供多个平台的二进制文件。
3. 使用 React 编写的数据查询 Web UI（嵌入在二进制文件中）
4. 兼容 Elasticsearch 的数据写入 API（单条记录和批量 API）
5. 开箱即用的身份认证
6. 无模式（Schema less）—— 无需预先定义 schema，同一索引中的不同文档可以拥有不同的字段。
7. 索引存储在磁盘上
8. 支持聚合
9. 可选启用的[日志索引](../pkg/logs/README.md)，支持事件时间规范化和日志搜索默认配置

# 文档

文档地址：[https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# 截图

## 搜索界面

![Search screen](../screenshots/search_screen.jpg)

## 用户管理界面

![Users screen](../screenshots/users_screen.jpg)

# 入门

## 快速开始

查看 [快速开始](https://zincsearch-docs.zinc.dev/quickstart/)

# 发布版本

ZedSearch 已有数百个生产环境部署。

## 构建

源码构建**默认仅包含 API**：不需要 Node.js、npm 或 `frontend/` 目录。
通过 `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`（或 `conf/zed.toml`）
设置首次启动的凭据，然后在仓库根目录运行：

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zedsearch/: go run main.go [web]
```

`web` 需要源码检出、Go、Node.js 24+ 和 npm。它会运行 `npm ci`、
`npm run build` 和 `go build -tags web`，生成 `test/zedsearch-web`
（Windows 上为 `.exe`）。生成的二进制文件内嵌了所有 UI 资源，可以复制到
没有前端文件或 Node.js 的其他主机上运行。启动时会保留调用者的工作目录、
配置和环境变量。已使用 `-tags web` 构建的二进制文件会直接启动而不会重新构建，
无论是否带有 `web` 参数。

手动构建：

```sh
go build -o zedsearch ./cmd/zedsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zedsearch ./cmd/zedsearch
```

仅 API 的节点在 `/` 返回版本 JSON；`/ui/` 和 Swagger UI 被禁用。
Web 构建会将 `/` 重定向到 `/ui/`，并在配置中启用时提供 Swagger。
`sh/build.sh`、发布的二进制文件和标准 Docker 镜像仍然包含 UI。

实验性的多节点文档存储位于 `distribution/cmd`，而不是空的 `cmd/distr/` 目录。
它从不需要前端资源：

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

每个节点需使用不同的 ID、数据目录、Raft 地址和 gRPC 地址；
关于加入集群和 TLS，请参阅 [distribution/README.md](../distribution/README.md)。这是一个
独立的复制文档存储，而不是分布式的 ZedSearch REST/UI 服务器。

- CI（lint、在 Linux/macOS/Windows 上运行 `go test`、覆盖率）：[`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml
- Nightly 开发镜像 `ghcr.io/vcaesar/zedsearch-dev:nightly`：[`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml
- 发布（`v*` 标签、goreleaser、`ghcr.io/vcaesar/zedsearch`）：[`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/release.yml

> **注意 —— Nightly / 开发镜像（推送）在 fork 仓库中会失败：**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/vcaesar/zedsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> 该工作流使用仓库的 `GITHUB_TOKEN` 登录 GHCR，而该令牌只能向运行工作流的仓库所有者名下推送包。
> 在 fork 中，该令牌无权访问 `vcaesar` 组织，因此推送被拒绝。请在 `vcaesar/zedsearch` 中运行该工作流，
> 或者将 `nightly.yml` 中的 `env.IMAGE` 改为 `ghcr.io/<your-owner>/zedsearch-dev`（并确保该包已关联到仓库 /
> 仓库拥有 `packages: write` 权限）。

# 社区

- 如何开发并为 ZedSearch 做贡献

  请查看[贡献指南](../CONTRIBUTING.md)。另请查看[路线图](https://zincsearch-docs.zinc.dev/roadmap/)

# 示例

你可以使用 ZedSearch 索引和搜索任何数据。以下是一些社区成员使用 zedsearch 索引和搜索 Enron 邮件数据集的示例：

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
