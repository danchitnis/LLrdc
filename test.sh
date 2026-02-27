#!/usr/bin/env bash

set -euo pipefail

cleanup_containers() {
    # Remove any containers created from the llrdc image.
    # (Tests start the server via `npm start` -> `docker run`.)
    docker rm -f $(docker ps -aq --filter ancestor=danchitnis/llrdc:latest) 2>/dev/null || true
}

cleanup() {
    echo "Cleaning up docker containers..."
    sleep 1 # Give node time to terminate docker run commands gracefully
    cleanup_containers
}
trap cleanup EXIT

# Build the docker image
./docker-build.sh

# Run Playwright specs one-by-one (serial) and cleanup containers after each.
PLAYWRIGHT_ARGS=()
TEST_FILES=()

for arg in "$@"; do
    case "$arg" in
        *.spec.ts|tests/*.ts|tests/**/*.ts)
            TEST_FILES+=("$arg")
            ;;
        *)
            PLAYWRIGHT_ARGS+=("$arg")
            ;;
    esac
done

if [ ${#TEST_FILES[@]} -eq 0 ]; then
    # Default: all Playwright spec files.
    while IFS= read -r f; do
        TEST_FILES+=("$f")
    done < <(find tests -maxdepth 1 -name "*.spec.ts" | sort)
fi

overall_exit=0
for spec in "${TEST_FILES[@]}"; do
    echo ""
    echo "===== Running $spec ====="
    cleanup_containers

    set +e
    npx playwright test "$spec" --workers=1 --reporter=line "${PLAYWRIGHT_ARGS[@]}"
    exit_code=$?
    set -e

    cleanup_containers

    if [ $exit_code -ne 0 ]; then
        overall_exit=$exit_code
    fi
done

exit $overall_exit
