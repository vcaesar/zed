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

[English](../README.md) | [简体中文](README.zh.md) | [繁體中文](README.zht.md) | [日本語](README.ja.md) | [한국어](README.ko.md) | [Français](README.fr.md) | [Deutsch](README.de.md) | Español | [Русский](README.ru.md) | [Português](README.pt.md)

# ZedSearch

ZedSearch es un motor de búsqueda que realiza indexación de texto completo. Es una alternativa ligera a Elasticsearch y funciona con una fracción de los recursos. Utiliza [bluge](https://github.com/blugelabs/bluge) (a través del fork [vcaesar/riot](https://github.com/vcaesar/riot)) como biblioteca de indexación subyacente.

Es muy simple y fácil de operar, a diferencia de Elasticsearch, que requiere entender y ajustar un par de docenas de parámetros. Puedes tener ZedSearch funcionando en 2 minutos.

Es un reemplazo directo de Elasticsearch si solo ingieres datos mediante APIs y buscas usando Kibana (ZedSearch no es compatible con Kibana. ZedSearch ofrece su propia interfaz).

Mira el siguiente video para una demostración rápida de ZedSearch.

[![Zinc Youtube](../screenshots/zinc-youtube.jpg)](https://www.youtube.com/watch?v=aZXtuVjt1ow)

# Por qué ZedSearch

Aunque Elasticsearch es un producto muy bueno, es complejo, requiere muchos recursos y tiene más de una década. Construí ZedSearch para que a la gente le resulte más fácil usar la indexación de búsqueda de texto completo sin mucho trabajo.

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

![Search screen](../docs/hots/search-26.png)

## Pantalla de índices

![Index screen](../docs/hots/index-26.png)

# Primeros pasos

## Inicio rápido

Consulta el [inicio rápido](https://zincsearch-docs.zinc.dev/quickstart/)

# Versiones

ZedSearch tiene cientos de instalaciones en producción.

## Compilación

Define `ZED_FIRST_ADMIN_USER` / `ZED_FIRST_ADMIN_PASSWORD` o configura `conf/zed.toml` (ver abajo) y ejecuta desde la raíz del repositorio:

```sh
go run cmd/zedsearch/main.go      # API-only, :4080
go run cmd/zedsearch/main.go web  # with the web UI (Node.js 24+ and npm)
```

`web` genera `test/zedsearch-web`, un binario independiente con la interfaz integrada.

### Configuración

La configuración se aplica en este orden: valores predeterminados integrados ([`pkg/config/default.toml`](../pkg/config/default.toml)), luego el archivo opcional `conf/zed.toml` del directorio de trabajo y, por último, las variables de entorno no vacías. Cada clave en mayúsculas es también una variable de entorno, p. ej. `zed_server_port` → `ZED_SERVER_PORT`.

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

- Incluye solo las claves que cambies; las claves agrupadas mantienen su encabezado de tabla de `default.toml` (`[Shard]`, `[S3]`, `[Etcd]`, ...).
- Las claves desconocidas hacen fallar el arranque. Las duraciones se expresan en nanosegundos y los tamaños en bytes.
- Las credenciales del primer administrador solo crean el usuario admin en el primer arranque; cambia la contraseña tras iniciar sesión y nunca hagas commit de credenciales reales.

# Comunidad

- Cómo desarrollar y contribuir a ZedSearch

  Consulta la [guía de contribución](../CONTRIBUTING.md). Consulta también la [hoja de ruta](https://zincsearch-docs.zinc.dev/roadmap/)

# Ejemplos

Puedes usar ZedSearch para indexar y buscar cualquier dato. Estos son algunos ejemplos que la comunidad ha creado para indexar y buscar el conjunto de datos de correos de Enron usando zedsearch:

1. https://github.com/jorgeloaiza48/Enron-Email-DataSet
1. https://github.com/jhojanperlaza/email_search_engine
1. https://github.com/carlosarraes/zinmail
1. https://github.com/devjopa/golab-search
1. https://github.com/avaco2312/zincsearch
1. https://github.com/paolorossig/email-indexer
1. https://github.com/ulimonte05/zincsearching
