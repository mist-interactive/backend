#!/bin/bash
set -e
DB_CONTAINER_ID=$(docker compose ps -q database)
if [ -n "$DB_CONTAINER_ID" ] && [ "$(docker inspect -f '{{.State.Running}}' "$DB_CONTAINER_ID" 2>/dev/null)" == "true" ]; then
    DB_WAS_ALREADY_RUNNING=true
    echo "Database container is already running. Skipping startup..."
else
    DB_WAS_ALREADY_RUNNING=false
    echo "Database container is not running. Spinning it up..."
    docker compose up -d database
fi

echo -n "Waiting for database to pass health checks..."
until [ "$(docker inspect -f '{{.State.Health.Status}}' $(docker compose ps -q database))" == "healthy" ]; do
    printf '.'
    sleep 1
done
echo -e "\nDatabase is accepting connections, running tests..."

cleanup() {
  echo "Cleaning up..."
  if [ "$DB_WAS_ALREADY_RUNNING" = false ]; then
    docker compose stop database
  fi
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
