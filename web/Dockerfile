# syntax=docker/dockerfile:1
# Grus web UI: Nuxt node-server build (BFF plus SPA) on a distroless Node runtime.
# Build from the repository root, so the image is built from the same context as the controller:
#   docker build --secret id=npm_token,env=NODE_AUTH_TOKEN -f web/Dockerfile -t grus-web .
# The build needs the secret `npm_token`: a token with read:packages, to install @onelitefeathernet/pavo from
# GitHub Packages. It is mounted for the install step only and never written into an image layer.
# Runtime settings come from the environment (NUXT_GRUS_API_URL, NUXT_GRUS_TOKEN), nothing is baked in.

FROM node:24-alpine AS build
WORKDIR /app
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.npmrc ./
# `nuxt prepare` (postinstall) needs the sources, so install without scripts first.
RUN --mount=type=secret,id=npm_token,env=NODE_AUTH_TOKEN pnpm install --frozen-lockfile --ignore-scripts
COPY web/ ./
RUN pnpm exec nuxt prepare && pnpm build

FROM gcr.io/distroless/nodejs24-debian12:nonroot
WORKDIR /app
COPY --from=build /app/.output ./.output
ENV NODE_ENV=production HOST=0.0.0.0 PORT=3000
EXPOSE 3000
# The distroless image uses node as its entrypoint. No writable path is needed, so readOnlyRootFilesystem works.
CMD ["/app/.output/server/index.mjs"]
