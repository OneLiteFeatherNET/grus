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

## License

Apache License 2.0. See [LICENSE](LICENSE).
