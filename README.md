# Grus

Grus is OneLiteFeather's minimal control plane for Minecraft server pools. It starts, watches and stops
server instances on Kubernetes, hands out their templates and status over a small HTTP API and an event
stream, and is meant to replace CloudNet for the network.

## Status

Prototype. The controller, the Kubernetes driver, the Velocity plugin and a Minestom demo server are being
built toward one demo deployment. Nothing here is released or production ready, and the API may change.

## Layout

- `cmd/grus`: the controller binary (`grus controller`).
- `cmd/grus-agent`: the node-side agent. It must not depend on `k8s.io/*`.
- `cmd/grus-stub-server`: a native example game server (no JVM) that speaks the server list and the Grus self API.
- `internal/`: domain model, controller, drivers, store and templates.
- `api/`: the HTTP API contract (OpenAPI).
- `java/`: Gradle build of the Java modules (SDK, Velocity plugin, Minestom demo).
- `deploy/helm/grus`: the Helm chart.

## Development

Go tests and checks:

```sh
go test ./...
```

Java build (Java 25):

```sh
cd java && ./gradlew build
```

Prototype images are built by hand from the `prototype-images` workflow (`workflow_dispatch`). They are
tagged `prototype-<short sha>` and pushed to the OneLiteFeather registry. Release images are not published
yet.

## Native example server

`cmd/grus-stub-server` is a small static Go binary that proves Grus runs servers which are not JVM processes.
It answers the Minecraft Java server list (status and ping), refuses logins with "This is a Grus stub server",
and, when a controller is configured, reports ready and player counts to the Grus self API.

```sh
go build -o grus-stub-server ./cmd/grus-stub-server
GRUS_PORT=25565 GRUS_URL=http://localhost:8080 GRUS_TOKEN=<instance token> ./grus-stub-server
```

| Variable | Default | Meaning |
| --- | --- | --- |
| `GRUS_PORT`, `SERVER_PORT` | `25565` | TCP port to listen on (`GRUS_PORT` wins). |
| `GRUS_URL`, `GRUS_TOKEN` | unset | Controller base URL and instance token. Both needed to report to Grus. |
| `GRUS_INSTANCE` | unset | Instance id, used in the default MOTD. |
| `GRUS_STUB_MOTD` | `Grus native stub` | Server-list description. |
| `GRUS_STUB_MAX` | `20` | Advertised max players, also sent in status reports. |

With `GRUS_URL` and `GRUS_TOKEN` set, the stub posts `/v1/self/ready` once the listener is up (retrying until the
controller accepts it), sends `PUT /v1/self/status` every 5 seconds, and on SIGTERM posts `/v1/self/shutdown`
best effort before exiting.

## Private servers

A pool with a `private` section (`quotaPerOwner`, default 1; `idleTimeout`, default 10m) is not autoscaled:
`scaling.min` and `scaling.buffer` must be 0 and instances are created on demand.

```sh
grus private create home --owner <player-uuid>   # starts the server, or shows the one that already runs
grus private list --owner <player-uuid>
```

`POST /v1/private-servers` returns the owner's live instance in the pool (200) or starts one (201); an owner
at the quota gets 409. `DELETE /v1/private-servers/{id}` drains the server. An instance without players for
`idleTimeout` is stopped and the `private.idle-stop` event is published.

World persistence is spec only for now: a later version snapshots the instance directory to a template on
stop and restores it on the next start.

## Region cells

A pool with a `sharding` section splits its world into cells (`cells[]` with chunk bounds `minX/minZ/maxX/maxZ`,
inclusive; `cellSize` in chunks defaults to 32 and is only used once cells are generated). The controller gives
each cell to exactly one Ready instance of the pool and stores it as the instance label `grus.olf/cells`. An
assignment stays while its instance is Ready; cells of a failed or lost instance move to the Ready instance with
the fewest cells. Every change publishes `pool.cells`. Splitting and merging cells with the load is spec only.

```sh
grus cells world
curl -H "Authorization: Bearer $TOKEN" "$GRUS_URL/v1/pools/world/cells/at?x=520&z=10"
```

`GET /v1/pools/{pool}/cells` lists cell, bounds, instance and address; `GET /v1/pools/{pool}/cells/at?x=&z=` takes
block coordinates (chunk = block >> 4) and answers with the owning cell (404 when no cell covers the block, 409
while the cell has no Ready instance). The Velocity plugin adds `/region <x> <z>` (permission `grus.region`) for
the pool named in `GRUS_SHARD_POOL`.

## License

Apache License 2.0. See [LICENSE](LICENSE).
