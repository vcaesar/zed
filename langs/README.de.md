[![CI](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml/badge.svg)](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zincsearch/zincsearch.svg)](https://pkg.go.dev/github.com/zincsearch/zincsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zincsearch/zincsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/zincsearch/zincsearch)](https://github.com/zincsearch/zincsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/zincsearch/zincsearch/branch/main/graph/badge.svg)](https://codecov.io/github/zincsearch/zincsearch)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | Deutsch | [Español](README.es.md) | [Русский](README.ru.md) | [Português](README.pt.md)

❗Hinweis: Wenn Ihr Anwendungsfall die Log-Suche (Anwendungs- und Sicherheitslogs) statt der App-Suche (Implementierung einer Suchfunktion in Ihrer Anwendung oder Website) ist, sollten Sie sich das in Rust geschriebene Projekt [openobserve/openobserve](https://github.com/openobserve/openobserve) ansehen, das speziell für die Log-Suche entwickelt wurde.

# ZincSearch

ZincSearch ist eine Suchmaschine für Volltextindizierung. Sie ist eine leichtgewichtige Alternative zu Elasticsearch und benötigt nur einen Bruchteil der Ressourcen. Als zugrunde liegende Indizierungsbibliothek wird [bluge](https://github.com/blugelabs/bluge) (über den Fork [vcaesar/riot](https://github.com/vcaesar/riot)) verwendet.

Im Gegensatz zu Elasticsearch, bei dem man ein paar Dutzend Stellschrauben verstehen und abstimmen muss, ist sie sehr einfach zu betreiben. Sie können ZincSearch in 2 Minuten zum Laufen bringen.

Sie ist ein direkter Ersatz für Elasticsearch, wenn Sie nur Daten über APIs einspielen und mit Kibana suchen (Kibana wird von ZincSearch nicht unterstützt. ZincSearch bietet eine eigene Oberfläche).

Im folgenden Video sehen Sie eine kurze Demo von ZincSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Warum ZincSearch

Elasticsearch ist zwar ein sehr gutes Produkt, aber es ist komplex, benötigt viele Ressourcen und ist über zehn Jahre alt. Ich habe ZincSearch entwickelt, damit Volltext-Suchindizierung ohne großen Aufwand einfacher nutzbar wird.

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

![Search screen](../screenshots/search_screen.jpg)

## Benutzerverwaltung

![Users screen](../screenshots/users_screen.jpg)

# Erste Schritte

## Schnellstart

Siehe [Schnellstart](https://zincsearch-docs.zinc.dev/quickstart/)

# Releases

ZincSearch hat Hunderte von Produktionsinstallationen.

## Build

Builds aus dem Quellcode sind **standardmäßig nur API**: Node.js, npm oder das Verzeichnis `frontend/` werden nicht benötigt.
Legen Sie die Zugangsdaten für den ersten Start über `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
(oder `conf/zinc.toml`) fest und starten Sie dann im Wurzelverzeichnis des Repositorys:

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

`web` erfordert einen Checkout des Quellcodes, Go, Node.js 24+ und npm. Es führt `npm ci`,
`npm run build` und `go build -tags web` aus und erzeugt `test/zincsearch-web`
(`.exe` unter Windows). Die resultierende Binärdatei bettet alle UI-Assets ein und kann
auf einen anderen Host ohne Frontend-Dateien oder Node.js kopiert werden. Beim Start bleiben das
Arbeitsverzeichnis, die Konfiguration und die Umgebung des Aufrufers erhalten. Eine bereits mit
`-tags web` gebaute Binärdatei startet direkt ohne Neubau, mit oder ohne das Argument `web`.

Für manuelle Builds:

```sh
go build -o zincsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zincsearch ./cmd/zincsearch
```

Reine API-Knoten liefern unter `/` Versions-JSON; `/ui/` und die Swagger-UI sind deaktiviert.
Web-Builds leiten `/` auf `/ui/` um und stellen Swagger bereit, wenn es in der Konfiguration aktiviert ist.
`sh/build.sh`, Release-Binärdateien und das Standard-Docker-Image enthalten weiterhin die Oberfläche.

Der experimentelle Multi-Node-Dokumentenspeicher befindet sich in `distribution/cmd`, nicht im
leeren Verzeichnis `cmd/distr/`. Er benötigt niemals Frontend-Assets:

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Verwenden Sie für jeden Knoten eine eigene ID, ein eigenes Datenverzeichnis, eine eigene Raft- und gRPC-Adresse;
siehe [distribution/README.md](../distribution/README.md) für Beitritte und TLS. Dies ist ein
unabhängiger replizierter Dokumentenspeicher, kein verteilter ZincSearch-REST/UI-Server.

- CI (Lint, `go test` unter Linux/macOS/Windows, Coverage): [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml
- Nightly-Dev-Image `ghcr.io/zincsearch/zincsearch-dev:nightly`: [`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml
- Release (`v*`-Tags, goreleaser, `ghcr.io/zincsearch/zincsearch`): [`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/release.yml

> **Hinweis — Nightly / Dev-Image (Push) schlägt bei Forks fehl:**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/zincsearch/zincsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> Der Workflow meldet sich mit dem `GITHUB_TOKEN` des Repositorys bei GHCR an, das Pakete nur unter dem
> Besitzer des Repositorys pushen kann, in dem der Workflow läuft. In einem Fork hat das Token keinen Zugriff auf die
> Organisation `zincsearch`, daher wird der Push abgelehnt. Führen Sie den Workflow entweder in `zincsearch/zincsearch` aus,
> oder ändern Sie `env.IMAGE` in `nightly.yml` auf `ghcr.io/<your-owner>/zincsearch-dev` (und stellen Sie sicher, dass das
> Paket mit dem Repository verknüpft ist / das Repository `packages: write` hat).

# ZincSearch vs. OpenObserve

| Funktion                   | ZincSearch                                                                 | OpenObserve                                                                                        |
| -------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Idealer Anwendungsfall     | App-Suche                                                                  | Logs, Metriken, Traces (unveränderliche Daten)                                                     |
| Speicher                   | Festplatte                                                                 | Festplatte, Objektspeicher (S3), GCS, MinIO, swift und mehr.                                       |
| Bevorzugter Anwendungsfall | App-Suche                                                                  | Observability (Logs, Metriken, Traces)                                                             |
| Max. Datenmenge            | Hunderte GB                                                                | Petabyte-Bereich                                                                                   |
| Hochverfügbarkeit          | Nicht verfügbar                                                            | Ja                                                                                                 |
| Open Source                | Ja                                                                         | Ja, [OpenObserve](https://github.com/openobserve/openobserve)                                      |
| ES-API-Kompatibilität      | Ja                                                                         | Ja                                                                                                 |
| GUI                        | Einfach                                                                    | Sehr fortgeschritten, inklusive Dashboards                                                         |
| Kosten                     | Open Source                                                                | Open Source                                                                                        |
| Loslegen                   | [Open-Source-Dokumentation](https://zincsearch-docs.zinc.dev/quickstart/)  | [Open-Source-Dokumentation](https://openobserve.ai/docs) oder [Cloud](https://cloud.openobserve.ai) |

# Community

- Wie man ZincSearch entwickelt und dazu beiträgt

  Siehe den [Leitfaden für Beiträge](../CONTRIBUTING.md). Siehe auch die [Roadmap](https://zincsearch-docs.zinc.dev/roadmap/)

# Beispiele

Mit ZincSearch können Sie beliebige Daten indizieren und durchsuchen. Hier sind einige Beispiele, mit denen Nutzer den Enron-E-Mail-Datensatz mit zincsearch indiziert und durchsucht haben:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
