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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | 한국어 | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch는 전문(full text) 색인을 수행하는 검색 엔진입니다. Elasticsearch의 경량 대안으로, 아주 적은 리소스로 실행됩니다. 기본 색인 라이브러리로 [bluge](https://github.com/blugelabs/bluge)([vcaesar/riot](https://github.com/vcaesar/riot) 포크를 통해)를 사용합니다.

이해하고 조정해야 할 수십 개의 설정이 필요한 Elasticsearch와 달리 매우 간단하고 운영하기 쉽습니다. ZedSearch는 2분 안에 설치하고 실행할 수 있습니다.

API로 데이터를 수집하고 Kibana로 검색하기만 한다면 Elasticsearch를 그대로 대체할 수 있습니다(ZedSearch는 Kibana를 지원하지 않습니다. ZedSearch는 자체 UI를 제공합니다).

ZedSearch의 간단한 데모는 아래 동영상을 확인하세요.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# 왜 ZedSearch인가

Elasticsearch는 매우 좋은 제품이지만 복잡하고 많은 리소스가 필요하며 10년도 더 된 제품입니다. 저는 사람들이 많은 작업 없이도 전문 검색 색인을 더 쉽게 사용할 수 있도록 ZedSearch를 만들었습니다.

# 기능:

go + gin + react

1. 전문 색인 기능 제공
2. 설치와 실행을 위한 단일 바이너리. 여러 플랫폼용 바이너리를 Releases에서 제공합니다.
3. React로 작성된 데이터 조회용 Web UI(바이너리에 내장)
4. 데이터 수집을 위한 Elasticsearch API 호환성(단일 레코드 및 벌크 API)
5. 기본 제공 인증
6. 스키마리스 - 스키마를 미리 정의할 필요가 없으며, 같은 인덱스 내의 서로 다른 문서가 서로 다른 필드를 가질 수 있습니다.
7. 인덱스를 디스크에 저장
8. 집계(aggregation) 지원
9. 이벤트 시간 정규화와 로그 검색 기본값을 갖춘 옵트인 [로그 인덱스](../pkg/logs/README.md)

# 문서

문서는 [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)에서 확인할 수 있습니다

# 스크린샷

## 검색 화면

![Search screen](../docs/hots/search-26.png)

## 인덱스 화면

![Index screen](../docs/hots/index-26.png)

# 시작하기

## 빠른 시작

[빠른 시작](https://zincsearch-docs.zinc.dev/quickstart/)을 확인하세요

# 릴리스

ZedSearch는 수백 개의 프로덕션 환경에 설치되어 있습니다.

## 빌드

`ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`를 설정하거나 `conf/zed.toml`을 구성한 뒤(아래 참조) 저장소 루트에서 실행합니다:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web`은 UI가 내장된 독립 실행형 바이너리 `test/zedsearch-web`를 생성합니다.

### 설정

설정은 다음 순서로 적용됩니다: 내장 기본값([`pkg/config/default.toml`](../pkg/config/default.toml)), 작업 디렉터리의 선택적 `conf/zed.toml`, 비어 있지 않은 환경 변수. 각 키를 대문자로 바꾼 이름이 환경 변수입니다(예: `zed_server_port` → `ZED_SERVER_PORT`).

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

- 변경할 키만 작성하세요. 그룹화된 키는 `default.toml`의 테이블 헤더(`[Shard]`, `[S3]`, `[Etcd]` 등)를 유지해야 합니다.
- 알 수 없는 키가 있으면 시작에 실패합니다. 기간은 나노초, 크기는 바이트 단위입니다.
- 초기 관리자 자격 증명은 첫 시작 시 관리자 사용자 생성에만 사용됩니다. 로그인 후 비밀번호를 변경하고 실제 자격 증명은 커밋하지 마세요.

# 커뮤니티

- ZedSearch 개발 및 기여 방법

  [기여 가이드](../CONTRIBUTING.md)를 확인하세요. [로드맵](https://zincsearch-docs.zinc.dev/roadmap/)도 확인하세요

# 예제

ZedSearch를 사용하여 어떤 데이터든 색인하고 검색할 수 있습니다. 다음은 사람들이 zedsearch를 사용해 Enron 이메일 데이터셋을 색인하고 검색한 예제입니다:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
