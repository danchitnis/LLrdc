#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/scripts/playwright-browser-env.sh"

LOCK_FILE="${TMPDIR:-/tmp}/llrdc-playwright.lock"
TEST_IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"

if [ -n "${CONTAINER_IMAGE:-}" ]; then
    TEST_IMAGE_NAME="${CONTAINER_IMAGE%:*}"
fi

require_playwright_runtime

if ! command -v docker >/dev/null 2>&1; then
    echo "docker was not found." >&2
    exit 1
fi

configure_installed_chrome

if command -v flock >/dev/null 2>&1; then
    exec 9>"${LOCK_FILE}"
    if ! flock -n 9; then
        echo "Another llrdc Playwright run is active; waiting for ${LOCK_FILE}..."
        flock 9
    fi
fi

cleanup_containers() {
    # Remove any containers created from the llrdc image.
    # Tests start the server through the maintained Docker runner.
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

include_discovered_spec() {
    local spec="$1"
    # The cross-host GPU connection smoke is intentionally driven by its
    # dedicated runner, which supplies the required capture mode and endpoint.
    # Keep the existing accelerator directory suites unchanged.
    if [[ "$spec" == *"/gpu_connection.spec.ts" ]] && [ -z "${LLRDC_CAPTURE_MODE:-}" ]; then
        return 1
    fi
    return 0
}

for arg in "$@"; do
    case "$arg" in
        *.spec.ts|tests/*.ts|tests/**/*.ts)
            TEST_FILES+=("$arg")
            ;;
        tests/*)
            while IFS= read -r f; do
                if include_discovered_spec "$f"; then
                    TEST_FILES+=("$f")
                fi
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
        case "$f" in
            tests/linux-cpu-browser/*|tests/cpu/*) ;;
            *) continue ;;
        esac
        if [[ "$f" == *"latency_matrix.spec.ts"* ]]; then
            continue
        fi
        if include_discovered_spec "$f"; then
            TEST_FILES+=("$f")
        fi
    done < <(find tests -name "*.spec.ts" | sort)
fi

needs_cpu_image=false
needs_intel_image=false

for spec in "${TEST_FILES[@]}"; do
    case "$spec" in
        tests/intel/*|tests/intel-browser/*)
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
    if [ ${#PLAYWRIGHT_ARGS[@]} -gt 0 ]; then
        npx playwright test "$spec" --workers=1 --reporter=line --timeout=60000 "${PLAYWRIGHT_ARGS[@]}"
    else
        npx playwright test "$spec" --workers=1 --reporter=line --timeout=60000
    fi
    exit_code=$?
    set -e

    cleanup_containers

    if [ $exit_code -ne 0 ]; then
        overall_exit=$exit_code
    fi
done

exit $overall_exit
