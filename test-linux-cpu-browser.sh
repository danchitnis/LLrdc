#!/usr/bin/env bash

set -euo pipefail

LOCK_FILE="${TMPDIR:-/tmp}/llrdc-playwright.lock"
TEST_IMAGE_NAME="${IMAGE_NAME:-danchitnis/llrdc}"

if [ -n "${CONTAINER_IMAGE:-}" ]; then
    TEST_IMAGE_NAME="${CONTAINER_IMAGE%:*}"
fi

configure_installed_chrome() {
    local platform
    local chrome_version
    local wayland_socket

    platform="$(uname -s)"

    if [ -z "${PLAYWRIGHT_CHROME_EXECUTABLE:-}" ]; then
        case "$platform" in
            Darwin)
                PLAYWRIGHT_CHROME_EXECUTABLE="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
                ;;
            Linux)
                PLAYWRIGHT_CHROME_EXECUTABLE="$(command -v google-chrome-stable || command -v google-chrome || true)"
                ;;
            *)
                echo "Unsupported Playwright host platform: ${platform}" >&2
                exit 1
                ;;
        esac
    fi

    if [ ! -x "$PLAYWRIGHT_CHROME_EXECUTABLE" ]; then
        echo "Installed Google Chrome was not found at: ${PLAYWRIGHT_CHROME_EXECUTABLE:-<unset>}" >&2
        exit 1
    fi

    if [ "$platform" = "Linux" ]; then
        XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
        WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}"
        XDG_SESSION_TYPE="wayland"
        wayland_socket="${WAYLAND_DISPLAY}"
        if [[ "$wayland_socket" != /* ]]; then
            wayland_socket="${XDG_RUNTIME_DIR}/${wayland_socket}"
        fi
        if [ ! -S "$wayland_socket" ]; then
            echo "Active Wayland socket was not found at: ${wayland_socket}" >&2
            echo "Run this test from nzxt5's graphical session, including through SSH with a login shell." >&2
            exit 1
        fi
        export XDG_RUNTIME_DIR WAYLAND_DISPLAY XDG_SESSION_TYPE
        echo "Wayland socket: ${wayland_socket}"
    fi

    export PLAYWRIGHT_CHROME_EXECUTABLE
    if [ "$platform" = "Darwin" ]; then
        chrome_version="Google Chrome $(defaults read '/Applications/Google Chrome.app/Contents/Info.plist' CFBundleShortVersionString 2>/dev/null || true)"
    else
        chrome_version="$($PLAYWRIGHT_CHROME_EXECUTABLE --version 2>/dev/null || true)"
    fi
    echo "Browser executable: ${PLAYWRIGHT_CHROME_EXECUTABLE}"
    echo "Browser version: ${chrome_version:-unknown}"
    echo "Browser mode: headed"
}

if ! command -v npx >/dev/null 2>&1; then
    echo "npx was not found. Start a login shell so the configured Node.js installation is available." >&2
    exit 1
fi

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
        case "$f" in
            tests/linux-cpu-browser/*|tests/cpu/*) ;;
            *) continue ;;
        esac
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
