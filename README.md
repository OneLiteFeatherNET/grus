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

## License

Apache License 2.0. See [LICENSE](LICENSE).
