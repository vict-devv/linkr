# syntax=docker/dockerfile:1

# Builder stage
FROM golang:1.26.4-alpine AS builder

WORKDIR /app/stats-api

# shared is a dependency for the stats-api
COPY ./shared/go.mod /app/shared/go.mod
COPY ./shared/go.sum /app/shared/go.sum
COPY ./stats-api/go.mod ./go.mod
COPY ./stats-api/go.sum ./go.sum

# BuildKit cache mounting for go mod download
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  go mod download

COPY ./shared /app/shared/
COPY ./stats-api/cmd/ ./cmd/
COPY ./stats-api/internal/ ./internal/

# BuildKit cache mounting for go build
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
  CGO_ENABLED=0 go build -ldflags="-s -w" -o ./stats-api ./cmd/stats-api/main.go

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot

# non-root for PSS
USER 65534:65534

WORKDIR /

COPY --from=builder /app/stats-api/stats-api ./

EXPOSE 8083

CMD [ "./stats-api" ]

