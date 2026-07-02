#!/bin/bash
set -e

docker compose up -d database
echo -n "Waiting for database to pass health checks..."
until [ "$(docker inspect -f '{{.State.Health.Status}}' $(docker compose ps -q database))" == "healthy" ]; do
    printf '.'
    sleep 1
done
echo -e "\nDatabase is accepting connections, running tests..."

cleanup() {
  echo "Cleaning up..."
  docker compose stop database
}
trap cleanup EXIT

cd go-server && \
env $(cat ../.env | grep -v '^#' | xargs) \
DB_PASSWORD=$(cat ../../secrets/postgres_user_pw.txt) \
DB_HOST=localhost \
go test -v ./handlers

env $(cat ../.env | grep -v '^#' | xargs) \
DB_PASSWORD=$(cat ../../secrets/postgres_user_pw.txt) \
DB_HOST=localhost \
go test -v ./models
