#!/bin/bash
set -e

MAX_WAIT=55
WAITED=0

if [ "$(docker inspect -f '{{.State.Health.Status}}' $(docker compose ps -q go-server 2>/dev/null) 2>/dev/null)" != "healthy" ]; then
    echo -n "Waiting for go-server to pass health checks..."
    until [ "$(docker inspect -f '{{.State.Health.Status}}' $(docker compose ps -q go-server 2>/dev/null) 2>/dev/null)" == "healthy" ]; do
        STATUS=$(docker inspect -f '{{.State.Health.Status}}' $(docker compose ps -q go-server 2>/dev/null) 2>/dev/null)
        if [ "$STATUS" == "unhealthy" ]; then
       echo -e "\nError: go-server reported unhealthy."
            exit 1
        fi
        if [ "$WAITED" -ge "$MAX_WAIT" ]; then
            echo -e "\nError: Timed out after ${MAX_WAIT}s."
            exit 1
        fi
        printf '.'
        sleep 1
        WAITED=$((WAITED + 1))
    done
    echo -e "\ngo-server is healthy, running tests..."
fi

docker compose exec -T go-server go test -v -count=1 ./handlers
docker compose exec -T go-server go test -race -v -count=1 ./handlers -run ConcurrentAccess
docker compose exec -T go-server go test -v -count=1 ./models
docker compose exec -T go-server go test -race -v -count=1 ./realtime
