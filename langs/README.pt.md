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

![Search screen](../screenshots/search_screen.jpg)

## Tela de gerenciamento de usuários

![Users screen](../screenshots/users_screen.jpg)

# Primeiros passos

## Início rápido

Veja o [início rápido](https://zincsearch-docs.zinc.dev/quickstart/)

# Versões

O ZedSearch tem centenas de instalações em produção.

## Build

Builds a partir do código-fonte são **somente API por padrão**: não é necessário Node.js, npm nem o diretório `frontend/`.
Defina as credenciais da primeira inicialização via `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD`
(ou `conf/zed.toml`) e então execute a partir da raiz do repositório:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zedsearch/: go run main.go [web]
```

`web` requer uma cópia do código-fonte, Go, Node.js 24+ e npm. Ele executa `npm ci`,
`npm run build` e `go build -tags web`, gerando `test/zedsearch-web`
(`.exe` no Windows). O binário resultante embute todos os recursos da interface e pode ser copiado
para outro host sem arquivos de frontend nem Node.js. A inicialização preserva o diretório de trabalho,
a configuração e o ambiente de quem a chama. Um binário já compilado com `-tags web`
inicia diretamente sem recompilar, com ou sem o argumento `web`.

Para builds manuais:

```sh
go build -o zedsearch ./cmd/zedsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zedsearch ./cmd/zedsearch
```

Nós somente API retornam o JSON de versão em `/`; `/ui/` e a interface do Swagger ficam desabilitados.
Builds web redirecionam `/` para `/ui/` e servem o Swagger quando habilitado na configuração.
`sh/build.sh`, os binários de release e a imagem Docker padrão continuam incluindo a interface.

O armazenamento de documentos multinó experimental fica em `distribution/cmd`, não no
diretório vazio `cmd/distr/`. Ele nunca requer recursos de frontend:

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Use um ID, diretório de dados, endereço Raft e endereço gRPC distintos para cada nó;
veja [distribution/README.md](../distribution/README.md) para entrada de nós e TLS. Este é
um armazenamento de documentos replicado independente, não um servidor REST/UI distribuído do ZedSearch.

- CI (lint, `go test` em Linux/macOS/Windows, cobertura): [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml
- Imagem de desenvolvimento nightly `ghcr.io/vcaesar/zedsearch-dev:nightly`: [`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml
- Release (tags `v*`, goreleaser, `ghcr.io/vcaesar/zedsearch`): [`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/release.yml

> **Nota — O push da imagem nightly / de desenvolvimento falha em forks:**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/vcaesar/zedsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> O workflow faz login no GHCR com o `GITHUB_TOKEN` do repositório, que só pode publicar pacotes sob o
> proprietário do repositório que executa o workflow. Em um fork, o token não tem acesso à organização `vcaesar`,
> então o push é negado. Execute o workflow a partir de `vcaesar/zedsearch`, ou altere `env.IMAGE` em
> `nightly.yml` para `ghcr.io/<your-owner>/zedsearch-dev` (e garanta que o pacote esteja vinculado ao repositório /
> que o repositório tenha `packages: write`).

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
