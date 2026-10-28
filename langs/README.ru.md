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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | Русский | [Português](README.pt.md)

# ZedSearch

ZedSearch — это поисковый движок с полнотекстовой индексацией. Это легковесная альтернатива Elasticsearch, которой требуется лишь малая доля ресурсов. В качестве базовой библиотеки индексации используется [bluge](https://github.com/blugelabs/bluge) (через форк [vcaesar/riot](https://github.com/vcaesar/riot)).

В отличие от Elasticsearch, где нужно разобраться и настроить пару десятков параметров, ZedSearch очень прост в эксплуатации. Его можно запустить за 2 минуты.

Он является прямой заменой Elasticsearch, если вы только загружаете данные через API и ищете с помощью Kibana (Kibana не поддерживается ZedSearch. ZedSearch предоставляет собственный интерфейс).

Посмотрите видео ниже, чтобы увидеть краткую демонстрацию ZedSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Почему ZedSearch

Хотя Elasticsearch — очень хороший продукт, он сложен, требует много ресурсов и существует уже более десяти лет. Я создал ZedSearch, чтобы людям было проще пользоваться полнотекстовой поисковой индексацией без лишней работы.

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

![Search screen](../docs/hots/search-26.png)

## Экран индексов

![Index screen](../docs/hots/index-26.png)

# Начало работы

## Быстрый старт

См. [Быстрый старт](https://zincsearch-docs.zinc.dev/quickstart/)

# Релизы

ZedSearch используется в сотнях производственных установок.

## Сборка

Задайте `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` и выполните из корня репозитория:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` создаёт `test/zedsearch-web` — самостоятельный бинарный файл со встроенным UI.

# Сообщество

- Как разрабатывать ZedSearch и вносить вклад

  См. [руководство по участию](../CONTRIBUTING.md). Также см. [дорожную карту](https://zincsearch-docs.zinc.dev/roadmap/)

# Примеры

ZedSearch можно использовать для индексации и поиска любых данных. Вот несколько примеров индексации и поиска по датасету писем Enron с помощью zedsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
