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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | Deutsch | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch ist eine Suchmaschine für Volltextindizierung. Sie ist eine leichtgewichtige Alternative zu Elasticsearch und benötigt nur einen Bruchteil der Ressourcen. Als zugrunde liegende Indizierungsbibliothek wird [bluge](https://github.com/blugelabs/bluge) (über den Fork [vcaesar/riot](https://github.com/vcaesar/riot)) verwendet.

Im Gegensatz zu Elasticsearch, bei dem man ein paar Dutzend Stellschrauben verstehen und abstimmen muss, ist sie sehr einfach zu betreiben. Sie können ZedSearch in 2 Minuten zum Laufen bringen.

Sie ist ein direkter Ersatz für Elasticsearch, wenn Sie nur Daten über APIs einspielen und mit Kibana suchen (Kibana wird von ZedSearch nicht unterstützt. ZedSearch bietet eine eigene Oberfläche).

Im folgenden Video sehen Sie eine kurze Demo von ZedSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Warum ZedSearch

Elasticsearch ist zwar ein sehr gutes Produkt, aber es ist komplex, benötigt viele Ressourcen und ist über zehn Jahre alt. Ich habe ZedSearch entwickelt, damit Volltext-Suchindizierung ohne großen Aufwand einfacher nutzbar wird.

# Funktionen:

go + gin + react

1. Bietet Volltextindizierung
2. Eine einzige Binärdatei für Installation und Betrieb. Binärdateien für mehrere Plattformen sind unter Releases verfügbar.
3. In React geschriebene Web-Oberfläche zum Abfragen von Daten (in die Binärdatei eingebettet)
4. Kompatibilität mit Elasticsearch-APIs für die Datenaufnahme (Einzeldatensatz- und Bulk-API)
5. Authentifizierung ab Werk
6. Schemalos – kein Schema muss vorab definiert werden, und verschiedene Dokumente im selben Index können unterschiedliche Felder haben.
7. Indexspeicherung auf der Festplatte
8. Unterstützung für Aggregationen
9. Optionale [Log-Indizes](../pkg/logs/README.md) mit Normalisierung der Ereigniszeit und Standardeinstellungen für die Log-Suche

# Dokumentation

Die Dokumentation ist unter [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/) verfügbar

# Screenshots

## Suchansicht

![Search screen](../docs/hots/search-26.png)

## Indexansicht

![Index screen](../docs/hots/index-26.png)

# Erste Schritte

## Schnellstart

Siehe [Schnellstart](https://zincsearch-docs.zinc.dev/quickstart/)

# Releases

ZedSearch hat Hunderte von Produktionsinstallationen.

## Build

Setzen Sie `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` oder konfigurieren Sie `conf/zed.toml` (siehe unten) und führen Sie im Repository-Stammverzeichnis aus:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` erzeugt `test/zedsearch-web`, ein eigenständiges Binary mit eingebetteter UI.

### Konfiguration

Einstellungen werden in dieser Reihenfolge angewendet: eingebaute Standardwerte ([`pkg/config/default.toml`](../pkg/config/default.toml)), dann die optionale `conf/zed.toml` im Arbeitsverzeichnis, dann nicht leere Umgebungsvariablen. Jeder Schlüssel ist in Großbuchstaben auch eine Umgebungsvariable, z. B. `zed_server_port` → `ZED_SERVER_PORT`.

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

- Tragen Sie nur die Schlüssel ein, die Sie ändern; gruppierte Schlüssel behalten ihren Tabellen-Header aus `default.toml` (`[Shard]`, `[S3]`, `[Etcd]`, ...).
- Unbekannte Schlüssel lassen den Start fehlschlagen. Zeitdauern sind in Nanosekunden, Größen in Bytes angegeben.
- Die Zugangsdaten des ersten Administrators legen nur beim ersten Start den Admin-Benutzer an; ändern Sie das Passwort nach der Anmeldung und committen Sie nie echte Zugangsdaten.

# Community

- Wie man ZedSearch entwickelt und dazu beiträgt

  Siehe den [Leitfaden für Beiträge](../CONTRIBUTING.md). Siehe auch die [Roadmap](https://zincsearch-docs.zinc.dev/roadmap/)

# Beispiele

Mit ZedSearch können Sie beliebige Daten indizieren und durchsuchen. Hier sind einige Beispiele, mit denen Nutzer den Enron-E-Mail-Datensatz mit zedsearch indiziert und durchsucht haben:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
