#Stage 1: Build executable
FROM golang:1.25-bookworm as go-builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o backend main.go

#Stage 2: execute
FROM debian:bookworm-slim

WORKDIR /app

COPY --from=go-builder /app/backend .

EXPOSE 8080

CMD ["./backend"]
