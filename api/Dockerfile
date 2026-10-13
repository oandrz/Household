# syntax=docker/dockerfile:1

# Every FROM names an exact version and its digest. The tag is for people;
# the digest is what Docker pulls, so a rebuilt upstream image cannot change
# what ships without a change to this file. The digest is the multi-platform
# one. Dependabot proposes the next version with its new digest.
#
# The Go version here must match the `go` line in go.mod. `make lint` reads
# its Go version from go.mod, so the two files are the only places to change.

# --- dev: hot reload -------------------------------------------------------
FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS dev
WORKDIR /src
# air and goose are pinned, not @latest: air's newest release already requires a
# newer Go than this image ships, and goose could do the same tomorrow. Pinning
# keeps `docker compose build` from breaking out from under us with no warning.
# Bump these deliberately after checking the target version's go.mod against the
# Go version of the base above.
RUN go install github.com/air-verse/air@v1.66.1 \
 && go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
COPY go.mod go.sum ./
RUN go mod download
COPY . .
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# --- builder ---------------------------------------------------------------
FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- admin: migrations and break-glass -------------------------------------
# The prod image below is deliberately a single binary with no shell, which
# also means a deployed install cannot migrate itself or be rescued. This
# target carries the two tools that do, on the same distroless base, so the
# production surface grows by two static binaries rather than by a Go
# toolchain. CGO_ENABLED=0 on the goose install is load-bearing: the alpine
# builder has no C compiler, and a dynamically linked binary would not run on
# distroless/static at all.
FROM builder AS admin-builder
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.27.3 \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl

FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS admin
COPY --from=admin-builder /out/adminctl /app/adminctl
COPY --from=admin-builder /go/bin/goose /app/goose
COPY --from=admin-builder /src/migrations /app/migrations
USER nonroot:nonroot
# No ENTRYPOINT on purpose: this image is two tools, not one service. The
# binary is named at run time -- `docker compose run --rm admin /app/adminctl …`

# --- prod ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS prod
COPY --from=builder /out/api /app/api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]
