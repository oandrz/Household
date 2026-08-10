# syntax=docker/dockerfile:1

# --- dev: hot reload -------------------------------------------------------
FROM golang:1.25.7-alpine AS dev
WORKDIR /src
# air and goose are pinned, not @latest: air's newest release already requires a
# newer Go than this image ships, and goose could do the same tomorrow. Pinning
# keeps `docker compose build` from breaking out from under us with no warning.
# Bump these deliberately after checking the target version's go.mod against the
# golang:1.25.7-alpine base above.
RUN go install github.com/air-verse/air@v1.66.1 \
 && go install github.com/pressly/goose/v3/cmd/goose@v3.27.3
COPY go.mod go.sum ./
RUN go mod download
COPY . .
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# --- builder ---------------------------------------------------------------
FROM golang:1.25.7-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# --- prod ------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS prod
COPY --from=builder /out/api /app/api
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/api"]

# --- admin: migrations and break-glass -------------------------------------
# The prod image above is deliberately a single binary with no shell, which
# also means a deployed install cannot migrate itself or be rescued. This
# target carries the two tools that do, on the same distroless base, so the
# production surface grows by two static binaries rather than by a Go
# toolchain. CGO_ENABLED=0 on the goose install is load-bearing: the alpine
# builder has no C compiler, and a dynamically linked binary would not run on
# distroless/static at all.
FROM builder AS admin-builder
RUN CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.27.3 \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/adminctl ./cmd/adminctl

FROM gcr.io/distroless/static-debian12:nonroot AS admin
COPY --from=admin-builder /out/adminctl /app/adminctl
COPY --from=admin-builder /go/bin/goose /app/goose
COPY --from=admin-builder /src/migrations /app/migrations
USER nonroot:nonroot
# No ENTRYPOINT on purpose: this image is two tools, not one service. The
# binary is named at run time -- `docker compose run --rm admin /app/adminctl …`
