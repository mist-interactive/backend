<<<<<<< Updated upstream
#Stage 1: Build executable
FROM golang:1.25-bookworm as base

=======
FROM golang:1.25-alpine as base
>>>>>>> Stashed changes
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download
<<<<<<< Updated upstream

#Stage 2a: hot reloading server for development
FROM base AS dev

  RUN go install github.com/air-verse/air@v1.61.7

  COPY . .

  EXPOSE 8080

  CMD ["air"]

#stage 2b: build executable
FROM base as builder
=======
>>>>>>> Stashed changes

FROM base AS dev
RUN go install github.com/air-verse/air@latest
COPY . .
CMD ["air"]

<<<<<<< Updated upstream
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o backend main.go

#Stage 3b: execute
FROM debian:bookworm-slim as production

WORKDIR /app

COPY --from=builder /app/backend .
=======
FROM base AS builder
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -o backend main.go
>>>>>>> Stashed changes

FROM alpine:3.20 AS runner
WORKDIR /app
COPY --from=builder /app/backend .
EXPOSE 8080
CMD ["./backend"]
