# syntax=docker/dockerfile:1

# Builder stage
FROM golang:1.26.4-alpine AS builder

WORKDIR /app/shortener-api

# shared is a dependency for the shortener-api
COPY ./shared/go.mod /app/shared/go.mod
COPY ./shared/go.sum /app/shared/go.sum
COPY ./shortener-api/go.mod ./go.mod
COPY ./shortener-api/go.sum ./go.sum

# BuildKit cache mounting for go mod download
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  go mod download

COPY ./shared /app/shared/
COPY ./shortener-api/cmd/ ./cmd/
COPY ./shortener-api/internal/ ./internal/

# BuildKit cache mounting for go build
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
  CGO_ENABLED=0 go build -ldflags="-s -w" -o ./shortener-api ./cmd/shortener-api/main.go

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot

# non-root for PSS
USER 65534:65534

WORKDIR /

COPY --from=builder /app/shortener-api/shortener-api ./

EXPOSE 8080

CMD [ "./shortener-api" ]

