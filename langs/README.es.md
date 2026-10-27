[![CI](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml)
[![Nightly](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml/badge.svg?branch=main)](https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml)
[![Release](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml/badge.svg)](https://github.com/zincsearch/zincsearch/actions/workflows/release.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/zincsearch/zincsearch.svg)](https://pkg.go.dev/github.com/zincsearch/zincsearch)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zincsearch/zincsearch)](../go.mod)
[![golangci-lint](https://img.shields.io/badge/linter-golangci--lint-blue)](https://golangci-lint.run/)
[![Release](https://img.shields.io/github/v/release/zincsearch/zincsearch)](https://github.com/zincsearch/zincsearch/releases/latest)
[![Docs](https://img.shields.io/badge/Docs-Docs-green)](https://zincsearch-docs.zinc.dev/) [![codecov](https://codecov.io/github/zincsearch/zincsearch/branch/main/graph/badge.svg)](https://codecov.io/github/zincsearch/zincsearch)

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | Español | [Русский](README.ru.md) | [Português](README.pt.md)

❗Nota: si tu caso de uso es la búsqueda de logs (logs de aplicaciones y de seguridad) en lugar de la búsqueda en aplicaciones (implementar una función de búsqueda en tu aplicación o sitio web), deberías revisar el proyecto [openobserve/openobserve](https://github.com/openobserve/openobserve), construido en Rust y diseñado específicamente para la búsqueda de logs.

# ZincSearch

ZincSearch es un motor de búsqueda que realiza indexación de texto completo. Es una alternativa ligera a Elasticsearch y funciona con una fracción de los recursos. Utiliza [bluge](https://github.com/blugelabs/bluge) (a través del fork [vcaesar/riot](https://github.com/vcaesar/riot)) como biblioteca de indexación subyacente.

Es muy simple y fácil de operar, a diferencia de Elasticsearch, que requiere entender y ajustar un par de docenas de parámetros. Puedes tener ZincSearch funcionando en 2 minutos.

Es un reemplazo directo de Elasticsearch si solo ingieres datos mediante APIs y buscas usando Kibana (ZincSearch no es compatible con Kibana. ZincSearch ofrece su propia interfaz).

Mira el siguiente video para una demostración rápida de ZincSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Por qué ZincSearch

Aunque Elasticsearch es un producto muy bueno, es complejo, requiere muchos recursos y tiene más de una década. Construí ZincSearch para que a la gente le resulte más fácil usar la indexación de búsqueda de texto completo sin mucho trabajo.

# Características:

go + gin + react

1. Proporciona capacidad de indexación de texto completo
2. Un único binario para la instalación y ejecución. Binarios disponibles en las releases para múltiples plataformas.
3. Interfaz web para consultar datos escrita en React (embebida en el binario)
4. Compatibilidad con las APIs de Elasticsearch para la ingesta de datos (API de registro único y bulk)
5. Autenticación lista para usar
6. Sin esquema: no es necesario definir un esquema por adelantado y diferentes documentos del mismo índice pueden tener campos distintos.
7. Almacenamiento de índices en disco
8. Soporte de agregaciones
9. [Índices de logs](../pkg/logs/README.md) opcionales con normalización del tiempo del evento y valores predeterminados para búsqueda de logs

# Documentación

La documentación está disponible en [https://zincsearch-docs.zinc.dev/](https://zincsearch-docs.zinc.dev/)

# Capturas de pantalla

## Pantalla de búsqueda

![Search screen](../screenshots/search_screen.jpg)

## Pantalla de gestión de usuarios

![Users screen](../screenshots/users_screen.jpg)

# Primeros pasos

## Inicio rápido

Consulta el [inicio rápido](https://zincsearch-docs.zinc.dev/quickstart/)

# Versiones

ZincSearch tiene cientos de instalaciones en producción.

## Compilación

Las compilaciones desde el código fuente son **solo API por defecto**: no se necesita Node.js, npm ni el directorio `frontend/`.
Define las credenciales del primer arranque mediante `ZINC_FIRST_ADMIN_USER` / `ZINC_FIRST_ADMIN_PASSWORD`
(o `conf/zinc.toml`) y luego ejecuta desde la raíz del repositorio:

```sh
go run cmd/zincsearch/main.go      # API-only, :4080
go run cmd/zincsearch/main.go web  # build and embed the console, then start
# Equivalently, from cmd/zincsearch/: go run main.go [web]
```

`web` requiere una copia del código fuente, Go, Node.js 24+ y npm. Ejecuta `npm ci`,
`npm run build` y `go build -tags web`, generando `test/zincsearch-web`
(`.exe` en Windows). El binario resultante incluye todos los recursos de la interfaz y puede copiarse
a otro host sin archivos de frontend ni Node.js. El arranque conserva el directorio de trabajo,
la configuración y el entorno de quien lo invoca. Un binario ya compilado con `-tags web`
arranca directamente sin recompilar, con o sin el argumento `web`.

Para compilaciones manuales:

```sh
go build -o zincsearch ./cmd/zincsearch  # API-only
(cd frontend && npm ci && npm run build)
go build -tags web -o zincsearch ./cmd/zincsearch
```

Los nodos solo API devuelven el JSON de versión en `/`; `/ui/` y la interfaz de Swagger están deshabilitados.
Las compilaciones web redirigen `/` a `/ui/` y sirven Swagger cuando está habilitado en la configuración.
`sh/build.sh`, los binarios de las releases y la imagen Docker estándar siguen incluyendo la interfaz.

El almacén de documentos multinodo experimental se encuentra en `distribution/cmd`, no en el
directorio vacío `cmd/distr/`. Nunca requiere recursos de frontend:

```sh
(cd distribution/cmd && go run main.go -id n1 -data ../../test/n1 -bootstrap)
```

Usa un ID, un directorio de datos, una dirección Raft y una dirección gRPC distintos para cada nodo;
consulta [distribution/README.md](../distribution/README.md) para la unión de nodos y TLS. Se trata de
un almacén de documentos replicado independiente, no de un servidor REST/UI de ZincSearch distribuido.

- CI (lint, `go test` en Linux/macOS/Windows, cobertura): [`.github/workflows/ci.yml`](../.github/workflows/ci.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/ci.yml
- Imagen de desarrollo nightly `ghcr.io/zincsearch/zincsearch-dev:nightly`: [`.github/workflows/nightly.yml`](../.github/workflows/nightly.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/nightly.yml
- Release (tags `v*`, goreleaser, `ghcr.io/zincsearch/zincsearch`): [`.github/workflows/release.yml`](../.github/workflows/release.yml) → https://github.com/zincsearch/zincsearch/actions/workflows/release.yml

> **Nota — El push de la imagen nightly / de desarrollo falla en los forks:**
>
> ```
> Error: buildx failed with: ERROR: failed to build: failed to solve: failed to push ghcr.io/zincsearch/zincsearch-dev:0.4.11-8792c59-dev: denied: permission_denied: The requested installation does not exist.
> ```
>
> El workflow inicia sesión en GHCR con el `GITHUB_TOKEN` del repositorio, que solo puede publicar paquetes bajo el
> propietario del repositorio que ejecuta el workflow. En un fork, el token no tiene acceso a la organización `zincsearch`,
> por lo que el push se deniega. Ejecuta el workflow desde `zincsearch/zincsearch`, o cambia `env.IMAGE` en
> `nightly.yml` a `ghcr.io/<your-owner>/zincsearch-dev` (y asegúrate de que el paquete esté vinculado al repositorio /
> de que el repositorio tenga `packages: write`).

# ZincSearch vs OpenObserve

| Característica              | ZincSearch                                                                  | OpenObserve                                                                                           |
| --------------------------- | --------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Caso de uso ideal           | Búsqueda en aplicaciones                                                    | Logs, métricas, trazas (datos inmutables)                                                             |
| Almacenamiento              | Disco                                                                       | Disco, objetos (S3), GCS, MinIO, swift y más.                                                         |
| Caso de uso preferido       | Búsqueda en aplicaciones                                                    | Observabilidad (logs, métricas, trazas)                                                               |
| Datos máximos soportados    | Cientos de GB                                                               | Escala de petabytes                                                                                   |
| Alta disponibilidad         | No disponible                                                               | Sí                                                                                                    |
| Código abierto              | Sí                                                                          | Sí, [OpenObserve](https://github.com/openobserve/openobserve)                                         |
| Compatibilidad con API de ES| Sí                                                                          | Sí                                                                                                    |
| Interfaz gráfica            | Básica                                                                      | Muy avanzada, incluye paneles                                                                         |
| Costo                       | Código abierto                                                              | Código abierto                                                                                        |
| Empezar                     | [Documentación de código abierto](https://zincsearch-docs.zinc.dev/quickstart/) | [Documentación de código abierto](https://openobserve.ai/docs) o [Cloud](https://cloud.openobserve.ai) |

# Comunidad

- Cómo desarrollar y contribuir a ZincSearch

  Consulta la [guía de contribución](../CONTRIBUTING.md). Consulta también la [hoja de ruta](https://zincsearch-docs.zinc.dev/roadmap/)

# Ejemplos

Puedes usar ZincSearch para indexar y buscar cualquier dato. Estos son algunos ejemplos que la comunidad ha creado para indexar y buscar el conjunto de datos de correos de Enron usando zincsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
