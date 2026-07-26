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
