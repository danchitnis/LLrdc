#!/usr/bin/env bash
set -euo pipefail

# Run the authoritative native latency benchmark from an SSH shell while
# attaching it to the already-running graphical user session.  The benchmark
# itself still refuses to publish a result unless the native surface receives
# real Wayland presentation feedback.

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ -n "${LLRDC_ARTEFACT_DIR:-}" && "${LLRDC_ARTEFACT_DIR}" != /* ]]; then
  export LLRDC_ARTEFACT_DIR="${ROOT_DIR}/${LLRDC_ARTEFACT_DIR}"
fi
UID_VALUE="$(id -u)"
RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/${UID_VALUE}}"

if [[ -z "${DBUS_SESSION_BUS_ADDRESS:-}" ]]; then
  export DBUS_SESSION_BUS_ADDRESS="unix:path=${RUNTIME_DIR}/bus"
fi

# SSH does not inherit the graphical session environment.  Import only the
# display/session variables needed by Wayland and GLib; do not eval arbitrary
# values from the user manager environment.
if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
  while IFS='=' read -r key value; do
    case "${key}" in
      XDG_RUNTIME_DIR|WAYLAND_DISPLAY|DISPLAY|XDG_SESSION_TYPE|XDG_CURRENT_DESKTOP|GNOME_DESKTOP_SESSION_ID|DESKTOP_SESSION)
        [[ -n "${value}" ]] && export "${key}=${value}"
        ;;
    esac
  done < <(systemctl --user show-environment)
fi

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-${RUNTIME_DIR}}"
export WAYLAND_DISPLAY="${WAYLAND_DISPLAY:-wayland-0}"
export XDG_SESSION_TYPE="${XDG_SESSION_TYPE:-wayland}"

DESTINATION_COMPOSITOR="${LLRDC_DESTINATION_COMPOSITOR:-labwc}"
if [[ "${DESTINATION_COMPOSITOR}" == "labwc" ]]; then
  if ! command -v docker >/dev/null 2>&1; then
    echo "❌ The isolated labwc destination requires Docker on nzxt5" >&2
    exit 1
  fi
  # This mode does not depend on GNOME being unlocked or even running.  It
  # creates a disposable headless labwc output with presentation-time support
  # and connects the native client to that socket over the local filesystem.
  export LLRDC_DESTINATION_COMPOSITOR="labwc"
  export LLRDC_SKIP_WESTON="0"
  export LLRDC_GNOME_ACTIVATE="0"
  export LLRDC_REQUIRE_CLIENT_FOCUS="0"
  export LLRDC_ARTEFACT_DIR="${LLRDC_ARTEFACT_DIR:-${ROOT_DIR}/.artefact/remote-wayland-$(date +%Y%m%d-%H%M%S)}"
  echo "▶ Destination: isolated headless labwc compositor (lock-independent)"
  echo "▶ Artefacts: ${LLRDC_ARTEFACT_DIR}"
  if [[ "${LLRDC_VALIDATE_SESSION_ONLY:-0}" == "1" ]]; then
    echo "✅ Isolated destination validation passed"
    exit 0
  fi
  exec "${ROOT_DIR}/tests/linux-wayland-native/benchmark-wayland-native-latency.sh" "$@"
fi

if [[ "${XDG_SESSION_TYPE}" != "wayland" ]]; then
  echo "❌ The active user session is not Wayland (XDG_SESSION_TYPE=${XDG_SESSION_TYPE})" >&2
  exit 1
fi
if [[ ! -S "${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}" ]]; then
  # A stopped nested compositor can leave WAYLAND_DISPLAY stale in the user
  # manager environment.  Prefer the active session's conventional socket
  # when the advertised socket no longer exists.
  for candidate in wayland-0 wayland-1 wayland-2; do
    if [[ -S "${XDG_RUNTIME_DIR}/${candidate}" ]]; then
      WAYLAND_DISPLAY="${candidate}"
      export WAYLAND_DISPLAY
      break
    fi
  done
fi
if [[ ! -S "${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}" ]]; then
  echo "❌ Wayland socket not found: ${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}" >&2
  echo "   Log in to the nzxt5 graphical session first, then rerun this command." >&2
  exit 1
fi

active_wayland=0
if command -v loginctl >/dev/null 2>&1; then
  while read -r session uid _rest; do
    [[ "${uid}" == "${UID_VALUE}" ]] || continue
    type="$(loginctl show-session "${session}" -p Type --value 2>/dev/null || true)"
    state="$(loginctl show-session "${session}" -p State --value 2>/dev/null || true)"
    if [[ "${type}" == "wayland" && "${state}" == "active" ]]; then
      active_wayland=1
      break
    fi
  done < <(loginctl list-sessions --no-legend 2>/dev/null || true)
fi
if [[ "${active_wayland}" != "1" ]]; then
  echo "❌ No active Wayland login session was found for uid ${UID_VALUE}" >&2
  echo "   An installed desktop is not enough; start GNOME locally or through RDP first." >&2
  exit 1
fi

locked=""
if [[ -n "${session:-}" ]]; then
  locked="$(loginctl show-session "${session}" -p LockedHint --value 2>/dev/null || true)"
fi
if [[ "${locked}" == "yes" ]]; then
  echo "❌ The active GNOME session is locked; Wayland presentation feedback will be discarded" >&2
  echo "   Unlock the physical session or connect through GNOME Remote Desktop/RDP, then rerun." >&2
  exit 1
fi

# The active GNOME compositor is the destination.  Do not create a nested
# Weston window from SSH: Mutter can keep that top-level window unfocused,
# which correctly produces discarded presentation feedback.
export LLRDC_DESTINATION_COMPOSITOR="gnome"
export LLRDC_SKIP_WESTON="${LLRDC_SKIP_WESTON:-1}"
export LLRDC_GNOME_ACTIVATE="${LLRDC_GNOME_ACTIVATE:-1}"
export LLRDC_ARTEFACT_DIR="${LLRDC_ARTEFACT_DIR:-${ROOT_DIR}/.artefact/remote-wayland-$(date +%Y%m%d-%H%M%S)}"

echo "▶ Wayland session: ${XDG_RUNTIME_DIR}/${WAYLAND_DISPLAY}"
echo "▶ Destination: active GNOME compositor (GNOME application activation enabled)"
echo "▶ Artefacts: ${LLRDC_ARTEFACT_DIR}"
if [[ "${LLRDC_VALIDATE_SESSION_ONLY:-0}" == "1" ]]; then
  echo "✅ Graphical session validation passed"
  exit 0
fi
exec "${ROOT_DIR}/tests/linux-wayland-native/benchmark-wayland-native-latency.sh" "$@"
