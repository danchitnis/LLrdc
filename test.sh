#!/usr/bin/env bash

cleanup() {
    echo "Cleaning up docker image..."
    sleep 2 # Give node time to terminate the docker run commands gracefully
    docker rm -f $(docker ps -aq --filter ancestor=danchitnis/llrdc:latest) 2>/dev/null || true
    docker rmi danchitnis/llrdc:latest || true
}
trap cleanup EXIT

set -e

# Build the docker image
./docker-build.sh

# Run the playwright tests, passing any arguments
npx playwright test "$@"
