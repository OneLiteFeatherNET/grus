# Grus web UI

Nuxt 4 with the Nitro `node-server` preset. Nitro is a thin backend-for-frontend (BFF): every request to
`/api/grus/*` is forwarded to the Grus controller at `/v1/*`, with the admin token added on the server. The token
never reaches the browser. Server-sent events (`/api/grus/v1/events`, later the live logs) are passed through
unbuffered. `GET /healthz` is the liveness probe.

The prototype has no login. Access is only through `kubectl port-forward`. Entra OIDC comes later (Grus ADR 07),
together with the authorization of the mutating actions.

## Run locally

Forward the controller in one terminal:

```sh
kubectl -n grus-dev port-forward svc/grus 8080:8080
```

Then start the dev server with the admin token (read it from the cluster secret, do not commit it):

```sh
cd web
pnpm install
NUXT_GRUS_TOKEN=... pnpm dev
```

Open http://localhost:3000. `NUXT_GRUS_API_URL` defaults to `http://localhost:8080`.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `NUXT_GRUS_API_URL` | `http://localhost:8080` | Base URL of the controller, without `/v1` |
| `NUXT_GRUS_TOKEN` | empty | Admin bearer token. Without it the BFF answers 503 |
| `PORT`, `HOST` | `3000`, `0.0.0.0` | Set by the container image; Nitro listens on them |

## Scripts

- `pnpm dev`: development server
- `pnpm build`: production build into `.output`, start it with `node .output/server/index.mjs`
- `pnpm lint`, `pnpm typecheck`, `pnpm test`: the checks CI runs
- `pnpm gen:api`: regenerate `app/types/grus-api.d.ts` from `../api/openapi.yaml`. The test suite fails when the file is stale.

## Layout

- `server/`: the BFF (`routes/api/grus/[...path].ts`, `routes/healthz.ts`) and its pure helpers in `utils/`
- `app/lib/`: the typed client (`grus.ts`), the event core (`events.ts`), the instance state colours (`states.ts`),
  RFC 9457 problem handling (`problem.ts`)
- `app/composables/useGrusEvents.ts`: the single shared EventSource per tab, with a resync callback registry
- `app/components/`: `PageHeader`, `StatusChip`, `AppProblem`, `ConfirmDialog`; `app/components/m3/` holds the
  Material 3 primitives
- `app/pages/`: `/` (overview) is wired to the API; the other pages are placeholders
- `i18n/locales/{de,en}/<namespace>.json`: one namespace per page (`overview`, `pools`, `instances`, `templates`,
  `events`) plus `common`. German is the default.

## Container

```sh
docker build -f web/Dockerfile -t grus-web .   # from the repository root
docker run --rm -p 3000:3000 -e NUXT_GRUS_API_URL=... -e NUXT_GRUS_TOKEN=... grus-web
```

The image runs as non-root on distroless Node 24 and listens on port 3000.
