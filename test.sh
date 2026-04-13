#!/usr/bin/env bash

set -euo pipefail

LOCK_FILE="${TMPDIR:-/tmp}/llrdc-playwright.lock"
TEST_IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"

if [ -n "${CONTAINER_IMAGE:-}" ]; then
    TEST_IMAGE_NAME="${CONTAINER_IMAGE%:*}"
fi

if command -v flock >/dev/null 2>&1; then
    exec 9>"${LOCK_FILE}"
    if ! flock -n 9; then
        echo "Another llrdc Playwright run is active; waiting for ${LOCK_FILE}..."
        flock 9
    fi
fi

cleanup_containers() {
    # Remove any containers created from the llrdc image.
    # (Tests start the server via `npm start` -> `docker run`.)
    local latest_containers
    local intel_containers

    latest_containers=$(docker ps -aq --filter "ancestor=${TEST_IMAGE_NAME}:latest")
    intel_containers=$(docker ps -aq --filter "ancestor=${TEST_IMAGE_NAME}:intel")

    if [ -n "$latest_containers" ]; then
        docker kill $latest_containers 2>/dev/null || true
        docker rm -f $latest_containers 2>/dev/null || true
    fi

    if [ -n "$intel_containers" ]; then
        docker kill $intel_containers 2>/dev/null || true
        docker rm -f $intel_containers 2>/dev/null || true
    fi
}

cleanup() {
    echo "Cleaning up docker containers..."
    sleep 1 # Give node time to terminate docker run commands gracefully
    cleanup_containers
}
trap cleanup EXIT

# Run Playwright specs one-by-one (serial) and cleanup containers after each.
PLAYWRIGHT_ARGS=()
TEST_FILES=()

for arg in "$@"; do
    case "$arg" in
        *.spec.ts|tests/*.ts|tests/**/*.ts)
            TEST_FILES+=("$arg")
            ;;
        tests/*)
            while IFS= read -r f; do
                TEST_FILES+=("$f")
            done < <(find "$arg" -name "*.spec.ts" | sort)
            ;;
        *)
            PLAYWRIGHT_ARGS+=("$arg")
            ;;
    esac
done

if [ ${#TEST_FILES[@]} -eq 0 ]; then
    # Default: all Playwright spec files, excluding latency_matrix.
    while IFS= read -r f; do
        if [[ "$f" == *"latency_matrix.spec.ts"* ]]; then
            continue
        fi
        TEST_FILES+=("$f")
    done < <(find tests -name "*.spec.ts" | sort)
fi

needs_cpu_image=false
needs_intel_image=false

for spec in "${TEST_FILES[@]}"; do
    case "$spec" in
        tests/intel/*)
            needs_intel_image=true
            ;;
        *)
            needs_cpu_image=true
            ;;
    esac
done

if [ "$needs_cpu_image" = "true" ]; then
    IMAGE_NAME="${TEST_IMAGE_NAME}" IMAGE_TAG="latest" ./docker-build.sh
fi

if [ "$needs_intel_image" = "true" ]; then
    IMAGE_NAME="${TEST_IMAGE_NAME}" IMAGE_TAG="intel" ./docker-build.sh --intel
fi

overall_exit=0
for spec in "${TEST_FILES[@]}"; do
    echo ""
    echo "===== Running $spec ====="
    cleanup_containers

    set +e
    npx playwright test "$spec" --workers=1 --reporter=line --timeout=60000 "${PLAYWRIGHT_ARGS[@]}"
    exit_code=$?
    set -e

    cleanup_containers

    if [ $exit_code -ne 0 ]; then
        overall_exit=$exit_code
    fi
done

exit $overall_exit
