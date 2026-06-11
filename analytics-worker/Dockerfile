# syntax=docker/dockerfile:1

# Builder stage
FROM golang:1.26.4-alpine AS builder

WORKDIR /app/analytics-worker

# shared is a dependency for the analytics-worker
COPY ./shared/go.mod /app/shared/go.mod
COPY ./shared/go.sum /app/shared/go.sum
COPY ./analytics-worker/go.mod ./go.mod
COPY ./analytics-worker/go.sum ./go.sum

# BuildKit cache mounting for go mod download
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  go mod download

COPY ./shared /app/shared/
COPY ./analytics-worker/cmd/ ./cmd/
COPY ./analytics-worker/internal/ ./internal/

# BuildKit cache mounting for go build
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
  --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
  CGO_ENABLED=0 go build -ldflags="-s -w" -o ./analytics-worker ./cmd/analytics-worker/main.go

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot

# non-root for PSS
USER 65534:65534

WORKDIR /

COPY --from=builder /app/analytics-worker/analytics-worker ./

EXPOSE 8081

CMD [ "./analytics-worker" ]
