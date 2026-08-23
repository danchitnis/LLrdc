#!/usr/bin/env bash

# Shared host preflight for headed Playwright runs using the installed Google Chrome.
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
                return 1
                ;;
        esac
    fi

    if [ ! -x "$PLAYWRIGHT_CHROME_EXECUTABLE" ]; then
        echo "Installed Google Chrome was not found at: ${PLAYWRIGHT_CHROME_EXECUTABLE:-<unset>}" >&2
        return 1
    fi

    if [ "$platform" = "Linux" ]; then
        XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
        WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}"
        XDG_SESSION_TYPE="wayland"
        wayland_socket="$WAYLAND_DISPLAY"
        if [[ "$wayland_socket" != /* ]]; then
            wayland_socket="${XDG_RUNTIME_DIR}/${wayland_socket}"
        fi
        if [ ! -S "$wayland_socket" ]; then
            echo "Active Wayland socket was not found at: ${wayland_socket}" >&2
            echo "Run this test from nzxt5's graphical session, including through SSH with a login shell." >&2
            return 1
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

require_playwright_runtime() {
    if ! command -v npx >/dev/null 2>&1; then
        echo "npx was not found. Start a login shell so the configured Node.js installation is available." >&2
        return 1
    fi
}
