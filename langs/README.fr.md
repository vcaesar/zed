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

![Search screen](../docs/hots/search-26.png)

## Écran des index

![Index screen](../docs/hots/index-26.png)

# Prise en main

## Démarrage rapide

Consultez le [démarrage rapide](https://zincsearch-docs.zinc.dev/quickstart/)

# Versions

ZedSearch compte des centaines d'installations en production.

## Compilation

Définissez `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` ou configurez `conf/zed.toml` (voir ci-dessous), puis exécutez depuis la racine du dépôt :

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` produit `test/zedsearch-web`, un binaire autonome intégrant l'interface.

### Configuration

Les paramètres sont appliqués dans cet ordre : valeurs par défaut intégrées ([`pkg/config/default.toml`](../pkg/config/default.toml)), puis le fichier optionnel `conf/zed.toml` du répertoire de travail, puis les variables d'environnement non vides. Chaque clé est aussi une variable d'environnement en majuscules, par ex. `zed_server_port` → `ZED_SERVER_PORT`.

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

- N'indiquez que les clés que vous modifiez ; les clés groupées conservent leur en-tête de table de `default.toml` (`[Shard]`, `[S3]`, `[Etcd]`, ...).
- Une clé inconnue fait échouer le démarrage. Les durées sont en nanosecondes, les tailles en octets.
- Les identifiants du premier administrateur ne servent qu'à créer l'utilisateur admin au premier démarrage ; changez le mot de passe après connexion et ne committez jamais de vrais identifiants.

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
