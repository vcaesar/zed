[![CI](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml/badge.svg)](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zincsearch/zincsearch.svg)](https://pkg.go.dev/github.com/zincsearch/zincsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zincsearch/zincsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/zincsearch/zincsearch)](https://github.com/zincsearch/zincsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/zincsearch/zincsearch/branch/main/graph/badge.svg)](https://codecov.io/github/zincsearch/zincsearch)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | Русский | [Português](README.pt.md)

❗Примечание: если ваш сценарий — поиск по логам (логи приложений и безопасности), а не поиск в приложении (реализация функции поиска в вашем приложении или на сайте), обратите внимание на проект [openobserve/openobserve](https://github.com/openobserve/openobserve), написанный на Rust и созданный специально для поиска по логам.

# ZincSearch

ZincSearch — это поисковый движок с полнотекстовой индексацией. Это легковесная альтернатива Elasticsearch, которой требуется лишь малая доля ресурсов. В качестве базовой библиотеки индексации используется [bluge](https://github.com/blugelabs/bluge) (через форк [vcaesar/riot](https://github.com/vcaesar/riot)).

В отличие от Elasticsearch, где нужно разобраться и настроить пару десятков параметров, ZincSearch очень прост в эксплуатации. Его можно запустить за 2 минуты.

Он является прямой заменой Elasticsearch, если вы только загружаете данные через API и ищете с помощью Kibana (Kibana не поддерживается ZincSearch. ZincSearch предоставляет собственный интерфейс).

Посмотрите видео ниже, чтобы увидеть краткую демонстрацию ZincSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Почему ZincSearch

Хотя Elasticsearch — очень хороший продукт, он сложен, требует много ресурсов и существует уже более десяти лет. Я создал ZincSearch, чтобы людям было проще пользоваться полнотекстовой поисковой индексацией без лишней работы.

# Возможности:

go + gin + react

1. Полнотекстовая индексация
2. Один бинарный файл для установки и запуска. Бинарные файлы для разных платформ доступны в релизах.
3. Веб-интерфейс для запросов к данным, написанный на React (встроен в бинарный файл)
4. Совместимость с API Elasticsearch для загрузки данных (API одиночной записи и bulk)
5. Встроенная аутентификация
6. Без схемы — не нужно заранее определять схему, а разные документы в одном индексе могут иметь разные поля.
7. Хранение индексов на диске
8. Поддержка агрегаций
9. Опциональные [индексы логов](../pkg/logs/README.md) с нормализацией времени событий и настройками по умолчанию для поиска по логам

# Документация

Документация доступна по адресу [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# Скриншоты

## Экран поиска

![Search screen](../screenshots/search_screen.jpg)

## Экран управления пользователями

![Users screen](../screenshots/users_screen.jpg)

# Начало работы

## Быстрый старт

См. [Быстрый старт](https://zincsearch-docs.zinc.dev/quickstart/)

# Релизы

ZincSearch используется в сотнях производственных установок.

## Сборка

Сборки из исходного кода по умолчанию **только API**: Node.js, npm и каталог `frontend/` не нужны.
Задайте учётные данные для первого запуска через `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
(или `conf/zinc.toml`), затем запустите из корня репозитория:

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

Для `web` нужны исходный код, Go, Node.js 24+ и npm. Команда выполняет `npm ci`,
`npm run build` и `go build -tags web`, создавая `test/zincsearch-web`
(`.exe` в Windows). Полученный бинарный файл содержит все ресурсы интерфейса и может быть скопирован
на другой хост без файлов фронтенда и Node.js. При запуске сохраняются рабочий каталог,
конфигурация и окружение вызывающего процесса. Бинарный файл, уже собранный с `-tags web`,
запускается сразу без пересборки — с аргументом `web` или без него.

Для ручной сборки:

```sh
go build -o zincsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zincsearch ./cmd/zincsearch
```

Узлы только с API возвращают JSON с версией по адресу `/`; `/ui/` и Swagger UI отключены.
Web-сборки перенаправляют `/` на `/ui/` и отдают Swagger, если он включён в конфигурации.
`sh/build.sh`, релизные бинарные файлы и стандартный Docker-образ по-прежнему включают интерфейс.

Экспериментальное многоузловое хранилище документов находится в `distribution/cmd`, а не в
пустом каталоге `cmd/distr/`. Ему никогда не нужны ресурсы фронтенда:

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Используйте для каждого узла свой ID, каталог данных, адрес Raft и адрес gRPC;
о присоединении узлов и TLS см. [distribution/README.md](../distribution/README.md). Это
независимое реплицируемое хранилище документов, а не распределённый REST/UI-сервер ZincSearch.

- CI (lint, `go test` на Linux/macOS/Windows, покрытие): [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml
- Ночной dev-образ `ghcr.io/zincsearch/zincsearch-dev:nightly`: [`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml
- Релиз (теги `v*`, goreleaser, `ghcr.io/zincsearch/zincsearch`): [`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/release.yml

> **Примечание — публикация (push) ночного / dev-образа не работает в форках:**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/zincsearch/zincsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> Workflow входит в GHCR с помощью `GITHUB_TOKEN` репозитория, который может публиковать пакеты только от имени
> владельца репозитория, в котором запущен workflow. В форке токен не имеет доступа к организации `zincsearch`,
> поэтому push отклоняется. Либо запускайте workflow из `zincsearch/zincsearch`, либо измените `env.IMAGE` в
> `nightly.yml` на `ghcr.io/<your-owner>/zincsearch-dev` (и убедитесь, что пакет привязан к репозиторию /
> у репозитория есть `packages: write`).

# ZincSearch и OpenObserve

| Характеристика               | ZincSearch                                                                  | OpenObserve                                                                                          |
| ---------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Идеальный сценарий           | Поиск в приложениях                                                         | Логи, метрики, трейсы (неизменяемые данные)                                                          |
| Хранилище                    | Диск                                                                        | Диск, объектное (S3), GCS, MinIO, swift и другие.                                                    |
| Предпочтительный сценарий    | Поиск в приложениях                                                         | Наблюдаемость (логи, метрики, трейсы)                                                                |
| Макс. объём данных           | Сотни ГБ                                                                    | Петабайты                                                                                            |
| Высокая доступность          | Нет                                                                         | Да                                                                                                   |
| Открытый исходный код        | Да                                                                          | Да, [OpenObserve](https://github.com/openobserve/openobserve)                                        |
| Совместимость с ES API       | Да                                                                          | Да                                                                                                   |
| GUI                          | Базовый                                                                     | Очень продвинутый, с дашбордами                                                                      |
| Стоимость                    | Открытый исходный код                                                       | Открытый исходный код                                                                                |
| Начать                       | [Документация](https://zincsearch-docs.zinc.dev/quickstart/)                | [Документация](https://openobserve.ai/docs) или [Cloud](https://cloud.openobserve.ai)                |

# Сообщество

- Как разрабатывать ZincSearch и вносить вклад

  См. [руководство по участию](../CONTRIBUTING.md). Также см. [дорожную карту](https://zincsearch-docs.zinc.dev/roadmap/)

# Примеры

ZincSearch можно использовать для индексации и поиска любых данных. Вот несколько примеров индексации и поиска по датасету писем Enron с помощью zincsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
