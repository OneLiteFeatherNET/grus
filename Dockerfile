# syntax=docker/dockerfile:1

# Controller image: one static binary (cmd/grus) on distroless, running as the nonroot user.

FROM golang:1.27.4-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/grus ./cmd/grus

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/grus /grus
USER 65532:65532
ENTRYPOINT ["/grus"]
CMD ["controller"]
