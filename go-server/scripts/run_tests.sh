#!/bin/bash
set -e

docker compose exec -T go-server go test -v -count=1 ./handlers
docker compose exec -T go-server go test -v -count=1 ./models
