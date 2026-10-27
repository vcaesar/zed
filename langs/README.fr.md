[![CI](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml/badge.svg)](https://github.com/vcaesar/zedsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/vcaesar/zedsearch.svg)](https://pkg.go.dev/github.com/vcaesar/zedsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/vcaesar/zedsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/vcaesar/zedsearch)](https://github.com/vcaesar/zedsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/vcaesar/zedsearch/branch/main/graph/badge.svg)](https://codecov.io/github/vcaesar/zedsearch)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | Français | [Deutsch](README.de.md) | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch est un moteur de recherche qui effectue de l'indexation plein texte. C'est une alternative légère à Elasticsearch qui fonctionne avec une fraction des ressources. Il utilise [bluge](https://github.com/blugelabs/bluge) (via le fork [vcaesar/riot](https://github.com/vcaesar/riot)) comme bibliothèque d'indexation sous-jacente.

Il est très simple et facile à exploiter, contrairement à Elasticsearch qui nécessite de comprendre et d'ajuster une vingtaine de paramètres. Vous pouvez démarrer ZedSearch en 2 minutes.

C'est un remplacement direct d'Elasticsearch si vous vous contentez d'ingérer des données via les API et de rechercher avec Kibana (Kibana n'est pas pris en charge par ZedSearch. ZedSearch fournit sa propre interface).

Regardez la vidéo ci-dessous pour une démonstration rapide de ZedSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Pourquoi ZedSearch

Bien qu'Elasticsearch soit un très bon produit, il est complexe, nécessite beaucoup de ressources et a plus de dix ans. J'ai créé ZedSearch pour qu'il soit plus facile d'utiliser l'indexation de recherche plein texte sans trop d'efforts.

# Fonctionnalités :

go + gin + react

1. Fournit une capacité d'indexation plein texte
2. Un seul binaire pour l'installation et l'exécution. Binaires disponibles dans les releases pour plusieurs plateformes.
3. Interface web de requêtage des données écrite en React (intégrée au binaire)
4. Compatibilité avec les API Elasticsearch pour l'ingestion de données (API d'enregistrement unique et bulk)
5. Authentification prête à l'emploi
6. Sans schéma - inutile de définir un schéma à l'avance, et différents documents d'un même index peuvent avoir des champs différents.
7. Stockage des index sur disque
8. Prise en charge des agrégations
9. [Index de logs](../pkg/logs/README.md) optionnels avec normalisation de l'horodatage des événements et paramètres par défaut pour la recherche de logs

# Documentation

La documentation est disponible sur [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# Captures d'écran

## Écran de recherche

![Search screen](../screenshots/search_screen.jpg)

## Écran de gestion des utilisateurs

![Users screen](../screenshots/users_screen.jpg)

# Prise en main

## Démarrage rapide

Consultez le [démarrage rapide](https://zincsearch-docs.zinc.dev/quickstart/)

# Versions

ZedSearch compte des centaines d'installations en production.

## Compilation

Les compilations depuis les sources sont **API uniquement par défaut** : ni Node.js, ni npm, ni le répertoire `frontend/` ne sont nécessaires.
Définissez les identifiants du premier démarrage via `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
(ou `conf/zinc.toml`), puis lancez depuis la racine du dépôt :

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

`web` nécessite une copie des sources, Go, Node.js 24+ et npm. Il exécute `npm ci`,
`npm run build` et `go build -tags web`, produisant `test/zincsearch-web`
(`.exe` sous Windows). Le binaire résultant intègre tous les fichiers de l'interface et peut être copié
sur un autre hôte sans fichiers frontend ni Node.js. Le démarrage conserve le répertoire de travail,
la configuration et l'environnement de l'appelant. Un binaire déjà compilé avec `-tags web`
démarre directement sans recompilation, avec ou sans l'argument `web`.

Pour une compilation manuelle :

```sh
go build -o zedsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zedsearch ./cmd/zincsearch
```

Les nœuds API uniquement renvoient le JSON de version sur `/` ; `/ui/` et l'interface Swagger sont désactivés.
Les compilations web redirigent `/` vers `/ui/` et servent Swagger lorsqu'il est activé dans la configuration.
`sh/build.sh`, les binaires de release et l'image Docker standard incluent toujours l'interface.

Le stockage de documents multi-nœuds expérimental se trouve dans `distribution/cmd`, et non dans le
répertoire vide `cmd/distr/`. Il ne nécessite jamais les fichiers frontend :

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Utilisez un ID, un répertoire de données, une adresse Raft et une adresse gRPC distincts pour chaque nœud ;
voir [distribution/README.md](../distribution/README.md) pour l'ajout de nœuds et TLS. Il s'agit
d'un stockage de documents répliqué indépendant, et non d'un serveur REST/UI ZedSearch distribué.

- CI (lint, `go test` sous Linux/macOS/Windows, couverture) : [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/ci.yml
- Image de développement nightly `ghcr.io/vcaesar/zedsearch-dev:nightly` : [`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/nightly.yml
- Release (tags `v*`, goreleaser, `ghcr.io/vcaesar/zedsearch`) : [`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/vcaesar/zedsearch/actions/workflows/release.yml

> **Remarque — Le push de l'image nightly / de développement échoue sur les forks :**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/vcaesar/zedsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> Le workflow se connecte à GHCR avec le `GITHUB_TOKEN` du dépôt, qui ne peut publier des packages que sous le
> propriétaire du dépôt qui exécute le workflow. Sur un fork, le jeton n'a pas accès à l'organisation `vcaesar`,
> le push est donc refusé. Exécutez le workflow depuis `vcaesar/zedsearch`, ou remplacez `env.IMAGE` dans
> `nightly.yml` par `ghcr.io/<your-owner>/zedsearch-dev` (et assurez-vous que le package est lié au dépôt /
> que le dépôt dispose de `packages: write`).

# Communauté

- Comment développer et contribuer à ZedSearch

  Consultez le [guide de contribution](../CONTRIBUTING.md). Consultez également la [feuille de route](https://zincsearch-docs.zinc.dev/roadmap/)

# Exemples

Vous pouvez utiliser ZedSearch pour indexer et rechercher n'importe quelles données. Voici quelques exemples créés par la communauté pour indexer et rechercher le jeu de données d'e-mails Enron avec zedsearch :

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
