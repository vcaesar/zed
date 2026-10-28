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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | Português

# ZedSearch

ZedSearch é um mecanismo de busca que faz indexação de texto completo. É uma alternativa leve ao Elasticsearch e roda usando uma fração dos recursos. Ele usa o [bluge](https://github.com/blugelabs/bluge) (através do fork [vcaesar/riot](https://github.com/vcaesar/riot)) como biblioteca de indexação subjacente.

É muito simples e fácil de operar, ao contrário do Elasticsearch, que exige entender e ajustar algumas dezenas de parâmetros. Você pode colocar o ZedSearch em funcionamento em 2 minutos.

É um substituto direto do Elasticsearch se você apenas ingere dados usando APIs e pesquisa usando o Kibana (o Kibana não é suportado pelo ZedSearch. O ZedSearch oferece sua própria interface).

Assista ao vídeo abaixo para uma demonstração rápida do ZedSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Por que ZedSearch

Embora o Elasticsearch seja um produto muito bom, ele é complexo, exige muitos recursos e tem mais de uma década. Eu criei o ZedSearch para que as pessoas possam usar indexação de busca de texto completo com mais facilidade, sem muito trabalho.

# Recursos:

go + gin + react

1. Oferece capacidade de indexação de texto completo
2. Binário único para instalação e execução. Binários disponíveis nas releases para várias plataformas.
3. Interface web para consultar dados escrita em React (embutida no binário)
4. Compatibilidade com as APIs do Elasticsearch para ingestão de dados (API de registro único e bulk)
5. Autenticação pronta para uso
6. Sem esquema - não é necessário definir um esquema antecipadamente e documentos diferentes no mesmo índice podem ter campos diferentes.
7. Armazenamento de índices em disco
8. Suporte a agregações
9. [Índices de logs](../pkg/logs/README.md) opcionais com normalização do horário do evento e padrões para busca em logs

# Documentação

A documentação está disponível em [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# Capturas de tela

## Tela de busca

![Search screen](../docs/hots/search-26.png)

## Tela de índices

![Index screen](../docs/hots/index-26.png)

# Primeiros passos

## Início rápido

Veja o [início rápido](https://zincsearch-docs.zinc.dev/quickstart/)

# Versões

O ZedSearch tem centenas de instalações em produção.

## Build

Defina `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` e execute a partir da raiz do repositório:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` gera `test/zedsearch-web`, um binário independente com a interface embutida.

# Comunidade

- Como desenvolver e contribuir com o ZedSearch

  Veja o [guia de contribuição](../CONTRIBUTING.md). Veja também o [roadmap](https://zincsearch-docs.zinc.dev/roadmap/)

# Exemplos

Você pode usar o ZedSearch para indexar e pesquisar quaisquer dados. Aqui estão alguns exemplos que a comunidade criou para indexar e pesquisar o conjunto de dados de e-mails da Enron usando o zedsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
