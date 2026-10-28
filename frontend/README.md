# ZedSearch React console

React 19, React Router, Tailwind CSS 4 and Vite. This replaces the active Vue/Quasar console; `../web/` remains as legacy source for reference.

## Development

Use Node.js 24 or newer:

```sh
cd frontend
npm ci
npm run dev
```

Open http://localhost:8080/ui/. Vite proxies `/api`, `/es`, `/version` and `/swagger` to http://localhost:4080. Set `ZED_API_TARGET` to change the development proxy target.

```sh
npm run lint       # strict TypeScript check
npm run test-once  # Vitest + Testing Library, mocked API regressions
npm run coverage
npm run build     # frontend/dist, embedded by Go
```

Plain Go builds/tests are API-only and do not require the frontend. Run `go run cmd/zedsearch/main.go web` from the repository root to build and embed the console automatically, or build these assets first and use `go build -tags web ./cmd/zedsearch` / `go test -tags web`. Web-enabled servers serve the console at `/ui/`, including direct route navigation. For reverse proxies, set `VITE_BASE_PATH=/prefix/ui/` at build time and strip `/prefix` before forwarding to ZedSearch. The API base defaults to the same prefix without `/ui/`; `VITE_API_ENDPOINT` explicitly overrides it. A separately hosted API must permit the frontend origin via CORS.

## Migration

- Login, persisted Basic authentication, 401 logout, cross-tab sign-out, protected routes.
- Search, relative/absolute/full time ranges, auto-refresh, index/field selection, result pagination, sorting, highlighting, JSON and histogram exports.
- Index creation, statistics and settings/mappings previews, bulk deletion.
- Template, user and role management, permission selection and validation.
- About/version, 404 and responsive navigation.
- 11 interface locales: English, German, Spanish, French, Portuguese, Japanese, Korean, Russian, Turkish, Simplified Chinese and Traditional Chinese. Choose a language on the login screen or in the console header; the choice is saved in a cookie. Otherwise, browser language preferences are used, with regional variants mapped to the supported language and English as the fallback.
- Search/index/template state survives navigation using React Activity.

Locale dictionaries live in `src/locales/`. New dictionaries must satisfy the English dictionary shape; register them in `src/locales/index.ts`. These cover the existing translation keys, not every interface string: some controls and server error messages remain in English.

Native accessible controls and JSON text editors replace Quasar/JSONEditor; an SVG histogram replaces ApexCharts. This is a behavior migration, not a pixel-identical copy. The retained `web/cypress` suite targets the old Vue DOM; React regression tests live under `src/`.

As in the original console, the persisted Basic token is password-equivalent. Use HTTPS and sign out on shared devices.
