#Stage 1: Build executable
FROM golang:1.25-bookworm as go-base

WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

#Stage 2a: hot reloading server for development
FROM go-base AS dev

  RUN go install github.com/air-verse/air@v1.61.7

  COPY . .

  EXPOSE 8080

  CMD ["air"]

#stage 2b: build executable
FROM go-base as go-builder

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o backend main.go

#Stage 3b: execute
FROM debian:bookworm-slim as production

WORKDIR /app

COPY --from=go-builder /app/backend .
EXPOSE 8080
CMD ["./backend"]
