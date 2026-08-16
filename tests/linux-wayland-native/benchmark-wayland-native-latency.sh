#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
PACKAGE_ROOT="${PACKAGE_ROOT:-${ROOT_DIR}/dist}"
PACKAGE_NAME="${PACKAGE_NAME:-llrdc-client-linux-amd64}"
CLIENT_BIN="${PACKAGE_ROOT}/${PACKAGE_NAME}/bin/llrdc-client"

ACCELERATOR="${LLRDC_ACCELERATOR:-nvidia}"
case "${ACCELERATOR}" in
  cpu)
    EXPECTED_ACCELERATOR="cpu"
    DEFAULT_VIDEO_CODEC="h264"
    BUILD_FLAGS=()
    RUN_FLAGS=()
    DEFAULT_MODES=(compat)
    ;;
  intel)
    EXPECTED_ACCELERATOR="intel"
    DEFAULT_VIDEO_CODEC="h264_qsv"
    BUILD_FLAGS=(--intel)
    RUN_FLAGS=(--intel)
    DEFAULT_MODES=(compat direct)
    ;;
  nvidia)
    EXPECTED_ACCELERATOR="nvidia"
    DEFAULT_VIDEO_CODEC="h264_nvenc"
    BUILD_FLAGS=(--nvidia)
    RUN_FLAGS=(--nvidia)
    DEFAULT_MODES=(compat direct)
    ;;
  *)
    echo "❌ Unsupported LLRDC_ACCELERATOR=${ACCELERATOR}; expected cpu, intel, or nvidia" >&2
    exit 2
    ;;
esac

MODE="${LLRDC_CAPTURE_MODE:-${DEFAULT_MODES[0]}}"
if [[ " ${DEFAULT_MODES[*]} " != *" ${MODE} "* ]]; then
  echo "❌ Capture mode ${MODE} is not supported for accelerator ${ACCELERATOR}" >&2
  exit 2
fi
FPS="${LLRDC_TARGET_FPS:-60}"
WINDOW_TITLE="${LLRDC_CLIENT_TITLE:-LLrdc Native Latency Bench}"
WINDOW_WIDTH="${LLRDC_CLIENT_WIDTH:-1920}"
WINDOW_HEIGHT="${LLRDC_CLIENT_HEIGHT:-1080}"
WARMUP_COUNT="${LLRDC_WARMUP_COUNT:-20}"
SAMPLE_COUNT="${LLRDC_SAMPLE_COUNT:-100}"
ARTIFACT_DIR="${LLRDC_ARTIFACT_DIR:-${ROOT_DIR}/artifacts/${ACCELERATOR}}"
WESTON_BACKEND="${LLRDC_WESTON_BACKEND:-wayland}"
WESTON_SOCKET="${LLRDC_WESTON_SOCKET:-llrdc-bench-$$}"
DESTINATION_COMPOSITOR="${LLRDC_DESTINATION_COMPOSITOR:-nested-weston}"
VIDEO_CODEC="${LLRDC_VIDEO_CODEC:-${DEFAULT_VIDEO_CODEC}}"
ACTUAL_ENCODER="unknown"
BANDWIDTH="${LLRDC_TARGET_BANDWIDTH_MBPS:-50}"
PRESENTATION_CLOCK_ID="${LLRDC_PRESENTATION_CLOCK_ID:-}"
SOURCE_PRESENTATION_CLOCK_ID=""
CLIENT_DPI="${LLRDC_CLIENT_DPI:-200}"
CLIENT_FULLSCREEN="${LLRDC_CLIENT_FULLSCREEN:-1}"
GNOME_ACTIVATE="${LLRDC_GNOME_ACTIVATE:-0}"
REQUIRE_CLIENT_FOCUS="${LLRDC_REQUIRE_CLIENT_FOCUS:-1}"
CLIENT_LAUNCHER_DIR=""
CLIENT_PID_FILE=""
DESTINATION_CONTAINER_NAME=""
DESTINATION_RUNTIME_DIR=""
CLIENT_XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-}"
DESTINATION_IMAGE="${LLRDC_DESTINATION_IMAGE:-}"
if [[ -z "${DESTINATION_IMAGE}" ]]; then
  if [[ "${ACCELERATOR}" == "cpu" ]]; then
    DESTINATION_IMAGE="danchitnis/llrdc:latest"
  else
    DESTINATION_IMAGE="danchitnis/llrdc:${ACCELERATOR}"
  fi
fi

MEASURED_MARKERS=()
SAMPLE_ID=0

get_free_port() {
  local port=0
  while :; do
    port=$((RANDOM % 1000 + 8000))
    if command -v ss >/dev/null 2>&1; then
      ss -Htan "( sport = :${port} )" | grep -q . || break
    elif command -v lsof >/dev/null 2>&1; then
      lsof -nP -iTCP:"${port}" -sTCP:LISTEN -t >/dev/null 2>&1 || break
    else
      break
    fi
  done
  printf '%s\n' "${port}"
}

kill_process_group() {
  local pid="$1"
  [[ -n "${pid}" ]] || return 0
  kill -TERM -- "-${pid}" >/dev/null 2>&1 || kill "${pid}" >/dev/null 2>&1 || true
  sleep 0.2
  kill -KILL -- "-${pid}" >/dev/null 2>&1 || kill -KILL "${pid}" >/dev/null 2>&1 || true
  wait "${pid}" >/dev/null 2>&1 || true
}

read_client_state() { curl -fsS "http://127.0.0.1:${CONTROL_PORT}/statez"; }
read_latest_client_sample() { curl -fsS "http://127.0.0.1:${CONTROL_PORT}/latencyz/latest"; }
read_probe_marker() { docker exec "${CONTAINER_NAME}" cat /tmp/llrdc-latency-probe.json | jq -r '.marker'; }
read_server_trace() { curl -fsS "http://127.0.0.1:${SERVER_PORT}/latencyz?marker=$1"; }

read_client_state_retry() {
  local state
  for _ in {1..20}; do
    state="$(read_client_state 2>/dev/null || true)"
    if [[ -n "${state}" ]] && printf '%s' "${state}" | jq -e . >/dev/null 2>&1; then
      printf '%s\n' "${state}"
      return 0
    fi
    sleep 0.1
  done
  printf '{}\n'
}

wait_for_client_ready() {
  for i in {1..45}; do
    local ready=""
    ready="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/readyz" 2>/dev/null || true)"
    local focus_ok='.windowHasFocus == true'
    if [[ "${REQUIRE_CLIENT_FOCUS}" == "0" ]]; then
      focus_ok='true'
    fi
    if printf '%s' "${ready}" | jq -e ".webtransportConnected == true and .windowBackend == \"wayland\" and .windowVisible == true and .windowHasSurface == true and (${focus_ok}) and .renderLoopStarted == true" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "❌ Native Wayland client did not become foreground/focused; compositor feedback would be discarded for an occluded surface" >&2
  return 1
}

start_weston() {
  echo "▶ Launching Weston bench..."
  if [[ "${DESTINATION_COMPOSITOR}" == "labwc" ]]; then
    echo "▶ Launching isolated headless labwc destination..."
    DESTINATION_CONTAINER_NAME="${LLRDC_DESTINATION_CONTAINER_NAME:-llrdc-wayland-destination-${SERVER_PORT}}"
    DESTINATION_RUNTIME_DIR="${ARTIFACT_DIR}/destination-runtime"
    mkdir -p "${DESTINATION_RUNTIME_DIR}"
    chmod 777 "${DESTINATION_RUNTIME_DIR}"
    docker rm -f "${DESTINATION_CONTAINER_NAME}" >/dev/null 2>&1 || true
    docker run --rm --detach --name "${DESTINATION_CONTAINER_NAME}" --user remote \
      --entrypoint labwc \
      -e XDG_RUNTIME_DIR=/run/llrdc-destination \
      -e WLR_BACKENDS=headless \
      -e WLR_HEADLESS_OUTPUTS=1 \
      -e WLR_LIBINPUT_NO_DEVICES=1 \
      -v "${DESTINATION_RUNTIME_DIR}:/run/llrdc-destination" \
      "${DESTINATION_IMAGE}" >"${ARTIFACT_DIR}/destination-container.id"
    docker logs -f "${DESTINATION_CONTAINER_NAME}" >"${ARTIFACT_DIR}/destination.log" 2>&1 &
    WESTON_SOCKET="wayland-0"
    PRESENTATION_CLOCK_ID="${LLRDC_PRESENTATION_CLOCK_ID:-1}"
    CLIENT_XDG_RUNTIME_DIR="${DESTINATION_RUNTIME_DIR}"
    REQUIRE_CLIENT_FOCUS="0"
    export WESTON_SOCKET PRESENTATION_CLOCK_ID CLIENT_XDG_RUNTIME_DIR REQUIRE_CLIENT_FOCUS
    for _ in {1..40}; do
      if [[ -S "${DESTINATION_RUNTIME_DIR}/${WESTON_SOCKET}" ]]; then
        return 0
      fi
      sleep 0.25
    done
    echo "❌ Isolated labwc destination did not create a Wayland socket" >&2
    return 1
  fi
  if [[ "${LLRDC_SKIP_WESTON:-0}" == "1" ]]; then
    WESTON_SOCKET="${LLRDC_WAYLAND_DISPLAY:-wayland-0}"
    PRESENTATION_CLOCK_ID="${LLRDC_PRESENTATION_CLOCK_ID:-1}"
    export WESTON_SOCKET PRESENTATION_CLOCK_ID
    return 0
  fi
  local clock_shim="${ARTIFACT_DIR}/libllrdc-monotonic-clock.so"
  if ! cc -shared -fPIC -O2 -o "${clock_shim}" "${ROOT_DIR}/tests/linux-wayland-native/force-monotonic-clock.c" -ldl; then
    echo "❌ Cannot build the monotonic-clock Weston shim" >&2
    return 1
  fi
  local weston_cmd=(weston "--backend=${WESTON_BACKEND}" "--socket=${WESTON_SOCKET}" "--width=${WINDOW_WIDTH}" "--height=${WINDOW_HEIGHT}" "--idle-time=0" "--log=${WESTON_LOG}")
  setsid env LD_PRELOAD="${clock_shim}" "${weston_cmd[@]}" >/dev/null 2>&1 &
  WESTON_PID=$!
  local socket_path="/tmp/llrdc-run/${WESTON_SOCKET}"
  if [[ "${WESTON_BACKEND}" == "wayland" ]]; then socket_path="${XDG_RUNTIME_DIR}/${WESTON_SOCKET}"; fi
  for _ in {1..20}; do
    if [[ -S "${socket_path}" ]]; then
      PRESENTATION_CLOCK_ID="$(sed -n 's/.*presentation clock: .* id \([0-9][0-9]*\).*/\1/p' "${WESTON_LOG}" | head -n1)"
      if [[ "${PRESENTATION_CLOCK_ID}" == "1" || "${PRESENTATION_CLOCK_ID}" == "4" ]]; then
        export PRESENTATION_CLOCK_ID
        return 0
      fi
    fi
    sleep 0.5
  done
  echo "❌ Weston did not advertise a supported monotonic presentation clock (id 1 or 4)" >&2
  exit 1
}

start_server() {
  echo "▶ Starting server in Docker..."
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  if [[ "${ACCELERATOR}" == "cpu" ]]; then
    CAPTURE_MODE="${MODE}" PORT="${SERVER_PORT}" HOST_PORT="${SERVER_PORT}" \
      VIDEO_CODEC="${VIDEO_CODEC}" FPS="${FPS}" BANDWIDTH="${BANDWIDTH}" \
      VBR=false RESOLUTION="${WINDOW_WIDTH}x${WINDOW_HEIGHT}" \
      USE_DEBUG_INPUT="${LLRDC_DEBUG_INPUT:-false}" \
      LLRDC_PRESENTATION_CLOCK_ID="${PRESENTATION_CLOCK_ID}" \
      "${ROOT_DIR}/docker-run.sh" --capture-mode "${MODE}" --host-net \
      --detach --name "${CONTAINER_NAME}" >/dev/null
  else
    CAPTURE_MODE="${MODE}" PORT="${SERVER_PORT}" HOST_PORT="${SERVER_PORT}" \
      VIDEO_CODEC="${VIDEO_CODEC}" FPS="${FPS}" BANDWIDTH="${BANDWIDTH}" \
      VBR=false RESOLUTION="${WINDOW_WIDTH}x${WINDOW_HEIGHT}" \
      USE_DEBUG_INPUT="${LLRDC_DEBUG_INPUT:-false}" \
      LLRDC_PRESENTATION_CLOCK_ID="${PRESENTATION_CLOCK_ID}" \
      "${ROOT_DIR}/docker-run.sh" "${RUN_FLAGS[0]}" --capture-mode "${MODE}" --host-net \
      --detach --name "${CONTAINER_NAME}" >/dev/null
  fi
  
  # Stream server logs to our local artifacts folder for debugging
  docker logs -f "${CONTAINER_NAME}" >"${ARTIFACT_DIR}/server.log" 2>&1 &
  
  for _ in {1..80}; do
    local ready
    ready="$(curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz" 2>/dev/null || true)"
    if [[ -n "${ready}" ]] && printf '%s' "${ready}" | jq -e --arg accelerator "${EXPECTED_ACCELERATOR}" --arg mode "${MODE}" --arg codec "${VIDEO_CODEC}" --argjson fps "${FPS}" '
      .acceleratorMode == $accelerator and .captureMode == $mode and .videoCodec == $codec and .framerate == $fps and .vbr == false
      and ($mode != "direct" or (.directBuffer.active == true and .directBuffer.captureMode == "direct"))
    ' >/dev/null 2>&1; then
        return 0
    fi
    sleep 0.25
  done
  exit 1
}

wait_for_actual_encoder() {
  local pattern actual
  if [[ "${ACCELERATOR}" == "cpu" ]]; then
    pattern='Starting wf-recorder capture: .* -c libx264'
    actual="libx264"
  elif [[ "${ACCELERATOR}" == "intel" ]]; then
    # The Intel DMA-BUF wf-recorder path maps the logical h264_qsv request to
    # the VAAPI encoder. Validate the process command, not only readyz's
    # logical codec name.
    pattern='Starting wf-recorder capture: .* -c h264_vaapi'
    actual="h264_vaapi"
  elif [[ "${MODE}" == "direct" ]]; then
    pattern='Starting NVIDIA native direct capture:'
    actual="nvidia_direct_capture_native"
  else
    pattern='Starting wf-recorder capture: .* -c h264_nvenc'
    actual="h264_nvenc"
  fi
  for _ in {1..80}; do
    if grep -E "${pattern}" "${ARTIFACT_DIR}/server.log" >/dev/null 2>&1; then
      ACTUAL_ENCODER="${actual}"
      return 0
    fi
    sleep 0.25
  done
  echo "❌ Server did not start the expected encoder backend (${actual})" >&2
  tail -80 "${ARTIFACT_DIR}/server.log" >&2 || true
  return 1
}

start_probe() {
  echo "▶ Launching remote latency probe..."
  docker exec "${CONTAINER_NAME}" rm -f /tmp/llrdc-latency-probe-next-sample-id /tmp/llrdc-latency-probe.json
  docker exec -u remote -d "${CONTAINER_NAME}" bash -lc \
    "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0 LLRDC_PRESENTATION_CLOCK_ID=${PRESENTATION_CLOCK_ID}; latency_probe >/tmp/latency-probe.log 2>&1"
  sleep 5
  SOURCE_PRESENTATION_CLOCK_ID="$(read_probe_state_clock 2>/dev/null || true)"
  if [[ "${SOURCE_PRESENTATION_CLOCK_ID}" != "${PRESENTATION_CLOCK_ID}" ]]; then
    echo "❌ Source and destination Wayland presentation clocks are incompatible (source=${SOURCE_PRESENTATION_CLOCK_ID:-unknown}, destination=${PRESENTATION_CLOCK_ID})" >&2
    return 1
  fi
}

read_probe_state_clock() {
  docker exec "${CONTAINER_NAME}" cat /tmp/llrdc-latency-probe.json | jq -r '.presentationClockId // 0'
}

start_client() {
  echo "▶ Launching native client..."
  local fullscreen_args=()
  if [[ "${CLIENT_FULLSCREEN}" == "1" ]]; then
    fullscreen_args+=(--fullscreen)
  fi
  if [[ "${GNOME_ACTIVATE}" == "1" && "${LLRDC_SKIP_WESTON:-0}" == "1" ]]; then
    # An SSH child has no GNOME startup-activation token.  Launching through
    # the active session bus gives Mutter a real application activation and
    # lets the native Wayland surface become foreground instead of discarded.
    if ! command -v gio >/dev/null 2>&1; then
      echo "❌ LLRDC_GNOME_ACTIVATE=1 requires gio (GLib) on the remote host" >&2
      return 1
    fi
    CLIENT_LAUNCHER_DIR="${ARTIFACT_DIR}/gnome-client-launch"
    CLIENT_PID_FILE="${CLIENT_LAUNCHER_DIR}/pid"
    mkdir -p "${CLIENT_LAUNCHER_DIR}"
    cat >"${CLIENT_LAUNCHER_DIR}/launch.sh" <<EOF
#!/usr/bin/env bash
set -euo pipefail
echo "\$\$" >"${CLIENT_PID_FILE}"
exec env SDL_VIDEODRIVER=wayland LLRDC_PRESENTATION_CLOCK_ID="${PRESENTATION_CLOCK_ID}" LLRDC_SKIP_INITIAL_CONFIG=1 XDG_RUNTIME_DIR="${CLIENT_XDG_RUNTIME_DIR}" WAYLAND_DISPLAY="${WESTON_SOCKET}" \\
  "${CLIENT_BIN}" \\
  --server "http://127.0.0.1:${SERVER_PORT}" \\
  --config "${CLIENT_CONFIG}" \\
  --control-addr "127.0.0.1:${CONTROL_PORT}" \\
  --title "${WINDOW_TITLE}" \\
  --width "${WINDOW_WIDTH}" \\
  --height "${WINDOW_HEIGHT}" \\
  --fps "${FPS}" \\
  --auto-start \\
  --latency-probe \\
  ${fullscreen_args[*]}
EOF
    chmod 700 "${CLIENT_LAUNCHER_DIR}/launch.sh"
    cat >"${CLIENT_LAUNCHER_DIR}/llrdc-client.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=LLrdc Native Latency Bench
Exec=${CLIENT_LAUNCHER_DIR}/launch.sh
Terminal=false
StartupNotify=true
StartupWMClass=llrdc-client
NoDisplay=true
EOF
    rm -f "${CLIENT_PID_FILE}"
    gio launch "${CLIENT_LAUNCHER_DIR}/llrdc-client.desktop" >"${CLIENT_LOG}" 2>&1
    for _ in {1..40}; do
      if [[ -s "${CLIENT_PID_FILE}" ]]; then
        CLIENT_PID="$(cat "${CLIENT_PID_FILE}")"
        break
      fi
      sleep 0.25
    done
    if [[ -z "${CLIENT_PID:-}" ]]; then
      echo "❌ GNOME did not start the activated native client" >&2
      return 1
    fi
  else
    setsid env SDL_VIDEODRIVER=wayland LLRDC_PRESENTATION_CLOCK_ID="${PRESENTATION_CLOCK_ID}" LLRDC_SKIP_INITIAL_CONFIG=1 XDG_RUNTIME_DIR="${CLIENT_XDG_RUNTIME_DIR}" \
      WAYLAND_DISPLAY="${WESTON_SOCKET}" \
      "${CLIENT_BIN}" \
      --server "http://127.0.0.1:${SERVER_PORT}" \
      --config "${CLIENT_CONFIG}" \
      --control-addr "127.0.0.1:${CONTROL_PORT}" \
      --title "${WINDOW_TITLE}" \
      --width "${WINDOW_WIDTH}" \
      --height "${WINDOW_HEIGHT}" \
      --fps "${FPS}" \
      --auto-start \
      --latency-probe \
      "${fullscreen_args[@]}" >"${CLIENT_LOG}" 2>&1 &
    CLIENT_PID=$!
  fi
  wait_for_client_ready
  CLIENT_PRESENTATION_CLOCK_ID="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/readyz" | jq -r '.presentationClockId // 0')"
  if [[ "${CLIENT_PRESENTATION_CLOCK_ID}" != "1" && "${CLIENT_PRESENTATION_CLOCK_ID}" != "4" ]]; then
    echo "❌ Native client did not advertise Wayland presentation clock" >&2
    return 1
  fi
  if [[ "${MODE}" == "direct" ]]; then
    for _ in {1..80}; do
      if curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz" | jq -e '.directBuffer.zeroCopyValidated == true' >/dev/null 2>&1; then return 0; fi
      sleep 0.25
    done
    return 1
  fi
}

wait_for_latest_brightness() {
  local target="$1"
  local min_presentation="$2"
  local timeout="$3"
  for i in $(seq 1 "${timeout}"); do
    local sample match
    sample="$(read_latest_client_sample)"
    if [[ "${target}" == "white" ]]; then
      match=$(printf '%s' "${sample}" | jq -e --argjson min "${min_presentation}" '.available != false and (.presentationAt // 0) > $min and (.brightness // -1) > 150' >/dev/null && echo 1 || echo 0)
    else
      match=$(printf '%s' "${sample}" | jq -e --argjson min "${min_presentation}" '.available != false and (.presentationAt // 0) > $min and (.brightness // 999) < 80' >/dev/null && echo 1 || echo 0)
    fi
    if [[ "${match}" == "1" ]]; then return 0; fi
    sleep 0.1
  done
  return 1
}

wait_for_presented_frame() {
  local timeout="$1"
  for _ in $(seq 1 "${timeout}"); do
    if read_latest_client_sample | jq -e '.available != false and (.compositorPresentedNs // 0) > 0' >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  echo "❌ Native client received decoded frames but no destination Wayland presented feedback; refusing to substitute render-submit timing" >&2
  echo "Client state: $(read_client_state 2>/dev/null || true)" >&2
  return 1
}

wait_for_marker_increment() {
  local previous_marker="$1"
  local timeout="$2"
  for _ in $(seq 1 "${timeout}"); do
    local current_marker
    current_marker="$(read_probe_marker)"
    if [[ "${current_marker}" =~ ^[0-9]+$ ]] && [[ "${current_marker}" -gt "${previous_marker}" ]]; then
      printf '%s\n' "${current_marker}"
      return 0
    fi
    sleep 0.1
  done
  return 1
}

wait_for_server_trace_identity() {
  local marker="$1"
  local timeout="$2"
  local sample_id="$3"
  for _ in $(seq 1 "${timeout}"); do
    local trace dispatch_time trace_sample_id source_presented
    trace="$(read_server_trace "${marker}" 2>/dev/null || true)"
    dispatch_time="$(printf '%s' "${trace}" | jq -r '.webTransportWriteEndNs // 0' 2>/dev/null || echo 0)"
    trace_sample_id="$(printf '%s' "${trace}" | jq -r '.sampleId // 0' 2>/dev/null || echo 0)"
    source_presented="$(printf '%s' "${trace}" | jq -r '.sourcePresentedNs // 0' 2>/dev/null || echo 0)"
    if [[ "${dispatch_time}" =~ ^[0-9]+$ ]] && (( dispatch_time > 0 )) && [[ "${source_presented}" =~ ^[1-9][0-9]*$ ]] && [[ "${trace_sample_id}" == "${sample_id}" ]]; then
      printf '%s\n' "${marker}"
      return 0
    fi
    sleep 0.1
  done
  return 1
}

wait_for_client_frame_identity() {
  local marker="$1"
  local timeout="$2"
  for _ in $(seq 1 "${timeout}"); do
    if read_client_state | jq -e --argjson m "${marker}" '
      any((.recentLatencySamples // [])[];
        (.probeMarker // 0) == $m and (.compositorPresentedNs // 0) > 0
      )
    ' >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.1
  done
  return 1
}

perform_sample() {
  local record_result="$1"
  local previous_marker="$2"
  local prior_presentation
  SAMPLE_ID=$((SAMPLE_ID + 1))
  prior_presentation="$(read_latest_client_sample | jq -r '.presentationAt // 0')"
  
  # 1. Reset to BLACK (top-left) - wiggle slightly first to force a screen change and ensure a new frame is generated
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"x":0.2,"y":0.2}' "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null
  sleep 0.1
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"x":0.1,"y":0.1}' "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null
  wait_for_latest_brightness "black" "${prior_presentation}" 120
  sleep 0.5

  # 2. SLOW VISUAL MOTION (Visible steps)
  local steps=10
  for i in $(seq 1 "${steps}"); do
    local x y
    x=$(printf "%.4f" "$(bc -l <<< "0.1 + (0.4 - 0.1) * ${i} / ${steps}")")
    y=$(printf "%.4f" "$(bc -l <<< "0.1 + (0.4 - 0.1) * ${i} / ${steps}")")
    curl -fsS -X POST -H "Content-Type: application/json" -d "{\"x\":${x},\"y\":${y}}" "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null
    sleep 0.05
  done
  sleep 0.2

  # 3. TIMED TRIGGER (The benchmark event). Put the pointer over the probe,
  # then use button-down: the probe increments its marker on button-down,
  # avoiding compositor-specific motion-crossing behavior.
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"x":0.5,"y":0.5}' "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null
  sleep 0.2
  curl -fsS -X POST -H "Content-Type: application/json" -d "{\"button\":0,\"action\":\"mousedown\",\"sampleId\":${SAMPLE_ID}}" "http://127.0.0.1:${CONTROL_PORT}/input/mousebtn" >/dev/null
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"button":0,"action":"mouseup"}' "http://127.0.0.1:${CONTROL_PORT}/input/mousebtn" >/dev/null

  local next_marker
  if ! next_marker="$(wait_for_marker_increment "${previous_marker}" 40)"; then
    echo "Probe state at trigger failure: $(docker exec "${CONTAINER_NAME}" cat /tmp/llrdc-latency-probe.json 2>/dev/null || true)" >&2
    echo "❌ Probe marker did not advance" >&2
    exit 1
  fi

  # 4. Wait for the exact decoded frame carrying this visual sample ID.
  local identity
  if ! identity="$(wait_for_server_trace_identity "${next_marker}" 40 "${SAMPLE_ID}")"; then
    echo "Server trace for marker ${next_marker}: $(read_server_trace "${next_marker}" 2>/dev/null || true)" >&2
    echo "⚠️  No server trace observed for marker ${next_marker}; it will be recorded as rejected" >&2
  fi
  if ! wait_for_client_frame_identity "${next_marker}" 40; then
    echo "⚠️  No presented destination frame observed for marker ${next_marker}; it will be recorded as rejected" >&2
  fi

  if [[ "${record_result}" == "1" ]]; then
    MEASURED_MARKERS+=("${next_marker}")
  fi
  CURRENT_MARKER="${next_marker}"
}

collect_results() {
  local client_state rejected=0 valid=0
  client_state="$(read_client_state_retry)"
  : >"${RESULTS_JSONL}"
  : >"${REJECTIONS_TSV}"
  {
    echo "Native Linux ${ACCELERATOR} latency benchmark"
    echo "Metric: native client input-send -> destination Wayland presentation feedback"
    echo "Accelerator=${ACCELERATOR} mode=${MODE} requestedCodec=${VIDEO_CODEC} actualEncoder=${ACTUAL_ENCODER} resolution=${WINDOW_WIDTH}x${WINDOW_HEIGHT} fps=${FPS} destination=${DESTINATION_COMPOSITOR}"
    echo "Clock: shared compositor monotonic clock (source/destination id ${PRESENTATION_CLOCK_ID}); no cross-clock offset correction"
    echo
  } | tee "${REPORT_TXT}"
  for marker in "${MEASURED_MARKERS[@]}"; do
    local server_trace sample sample_count reason
    server_trace="$(curl -sS "http://127.0.0.1:${SERVER_PORT}/latencyz?marker=${marker}" 2>/dev/null || true)"
    if [[ -z "${server_trace}" || "${server_trace}" == "null" ]]; then
      reason="missing_server_trace"
      printf '%s\t%s\n' "${marker}" "${reason}" >>"${REJECTIONS_TSV}"
      rejected=$((rejected + 1)); continue
    fi
    local trace_write_end_ns
    trace_write_end_ns="$(printf '%s' "${server_trace}" | jq -r '.webTransportWriteEndNs // 0')"
    sample="$(printf '%s' "${client_state}" | jq -c --argjson marker "${marker}" --argjson traceWriteEnd "${trace_write_end_ns}" '
      [(.recentLatencySamples // [])[]
        | select((.probeMarker // 0) == $marker and (.receiveNs // 0) >= ($traceWriteEnd // 0))
      ] | sort_by(.compositorPresentedNs // 0) | .[0] // empty
    ' || true)"
    sample_count="$(printf '%s' "${client_state}" | jq -r --argjson marker "${marker}" '[.recentLatencySamples // [] | .[] | select((.probeMarker // 0) == $marker)] | length' || true)"
    if [[ "${sample_count}" == "0" ]]; then
      reason="0_decoded_frames_for_marker"
      printf '%s\t%s\n' "${marker}" "${reason}" >>"${REJECTIONS_TSV}"
      rejected=$((rejected + 1)); continue
    fi
    if [[ -z "${sample}" ]]; then
      reason="missing_decoded_frame"
      printf '%s\t%s\n' "${marker}" "${reason}" >>"${REJECTIONS_TSV}"
      rejected=$((rejected + 1)); continue
    fi

    local sample_id input_ns received_ns injected_ns requested_ns drawn_ns commit_ns source_ns write_start_ns write_end_ns receive_ns decode_ns render_ns presented_ns source_clock client_clock
    sample_id="$(printf '%s' "${server_trace}" | jq -r '.sampleId // 0')"
    input_ns="$(printf '%s' "${server_trace}" | jq -r '.clientInputSendNs // 0')"
    received_ns="$(printf '%s' "${server_trace}" | jq -r '.serverInputReceivedNs // 0')"
    injected_ns="$(printf '%s' "${server_trace}" | jq -r '.serverInputInjectedNs // 0')"
    requested_ns="$(printf '%s' "${server_trace}" | jq -r '.probeRequestedNs // 0')"
    drawn_ns="$(printf '%s' "${server_trace}" | jq -r '.probeDrawnNs // 0')"
    commit_ns="$(printf '%s' "${server_trace}" | jq -r '.probeCommitNs // 0')"
    source_ns="$(printf '%s' "${server_trace}" | jq -r '.sourcePresentedNs // 0')"
    write_start_ns="$(printf '%s' "${server_trace}" | jq -r '.webTransportWriteStartNs // 0')"
    write_end_ns="$(printf '%s' "${server_trace}" | jq -r '.webTransportWriteEndNs // 0')"
    receive_ns="$(printf '%s' "${sample}" | jq -r '.receiveNs // 0')"
    decode_ns="$(printf '%s' "${sample}" | jq -r '.decodeReadyNs // 0')"
    render_ns="$(printf '%s' "${sample}" | jq -r '.renderSubmittedNs // 0')"
    presented_ns="$(printf '%s' "${sample}" | jq -r '.compositorPresentedNs // 0')"
    source_clock="$(printf '%s' "${server_trace}" | jq -r '.sourcePresentationClockId // 0')"
    client_clock="${CLIENT_PRESENTATION_CLOCK_ID:-0}"

    reason=""
    [[ "${sample_id}" =~ ^[1-9][0-9]*$ ]] || reason="sample_id_missing"
    (( input_ns > 0 && received_ns > 0 && injected_ns > 0 && requested_ns > 0 && drawn_ns > 0 && commit_ns > 0 && source_ns > 0 && write_start_ns > 0 && write_end_ns > 0 && receive_ns > 0 && decode_ns > 0 && render_ns > 0 && presented_ns > 0 )) || reason="missing_stage_timestamp"
    (( source_clock == client_clock && (source_clock == 1 || source_clock == 4) )) || reason="incompatible_presentation_clock"
    # probeDrawnNs is wl_surface.frame scheduling feedback, not a display
    # timestamp; it may arrive after compositor presentation. Keep it in the
    # report, but do not force it into the authoritative commit/present chain.
    (( input_ns <= received_ns && received_ns <= injected_ns && injected_ns <= requested_ns && requested_ns <= commit_ns && requested_ns <= drawn_ns && commit_ns <= source_ns && source_ns <= write_start_ns && write_start_ns <= write_end_ns && write_end_ns <= receive_ns && receive_ns <= decode_ns && decode_ns <= render_ns && render_ns <= presented_ns )) || reason="non_monotonic_trace"
    if [[ -n "${reason}" ]]; then
      if [[ "${reason}" == "non_monotonic_trace" || "${reason}" == "missing_stage_timestamp" ]]; then
        echo "Rejected marker ${marker} trace=${server_trace} client=${sample}" >&2
      fi
      printf '%s\t%s\n' "${marker}" "${reason}" >>"${REJECTIONS_TSV}"
      rejected=$((rejected + 1)); continue
    fi
    local total_ns
    total_ns=$((presented_ns - input_ns))
    jq -cn --argjson sampleId "${sample_id}" --argjson marker "${marker}" \
      --argjson clientInputSendNs "${input_ns}" --argjson serverInputReceivedNs "${received_ns}" \
      --argjson serverInputInjectedNs "${injected_ns}" --argjson probeRequestedNs "${requested_ns}" --argjson probeDrawnNs "${drawn_ns}" --argjson probeCommitNs "${commit_ns}" \
      --argjson sourcePresentedNs "${source_ns}" --argjson webTransportWriteStartNs "${write_start_ns}" \
      --argjson webTransportWriteEndNs "${write_end_ns}" --argjson receiveNs "${receive_ns}" \
      --argjson decodeReadyNs "${decode_ns}" --argjson renderSubmittedNs "${render_ns}" \
      --argjson compositorPresentedNs "${presented_ns}" --argjson totalNs "${total_ns}" \
      '{sampleId:$sampleId,marker:$marker,clientInputSendNs:$clientInputSendNs,serverInputReceivedNs:$serverInputReceivedNs,serverInputInjectedNs:$serverInputInjectedNs,probeRequestedNs:$probeRequestedNs,probeDrawnNs:$probeDrawnNs,probeCommitNs:$probeCommitNs,sourcePresentedNs:$sourcePresentedNs,webTransportWriteStartNs:$webTransportWriteStartNs,webTransportWriteEndNs:$webTransportWriteEndNs,receiveNs:$receiveNs,decodeReadyNs:$decodeReadyNs,renderSubmittedNs:$renderSubmittedNs,compositorPresentedNs:$compositorPresentedNs,totalNs:$totalNs}' >>"${RESULTS_JSONL}"
    printf 'marker=%s total=%.3fms control=%.3fms encode+capture=%.3fms transport=%.3fms decode=%.3fms render=%.3fms compositor=%.3fms\n' \
      "${marker}" "$((total_ns / 1000000))" "$(((received_ns - input_ns) / 1000000))" "$(((write_start_ns - source_ns) / 1000000))" "$(((receive_ns - write_end_ns) / 1000000))" "$(((decode_ns - receive_ns) / 1000000))" "$(((render_ns - decode_ns) / 1000000))" "$(((presented_ns - render_ns) / 1000000))" | tee -a "${REPORT_TXT}"
    valid=$((valid + 1))
  done
  local total=$((valid + rejected))
  if (( total == 0 || rejected * 100 > total * 2 )); then
    echo "❌ Rejected samples exceed 2% (${rejected}/${total})" | tee -a "${REPORT_TXT}" >&2
    exit 1
  fi
  echo "Valid samples=${valid}; rejected=${rejected}" | tee -a "${REPORT_TXT}"
}

cleanup() {
  [[ -n "${CLIENT_PID:-}" ]] && kill_process_group "${CLIENT_PID}"
  [[ -n "${CLIENT_LAUNCHER_DIR:-}" ]] && rm -rf "${CLIENT_LAUNCHER_DIR}" >/dev/null 2>&1 || true
  [[ -n "${WESTON_PID:-}" ]] && kill_process_group "${WESTON_PID}"
  if [[ "${LLRDC_KEEP_CONTAINERS:-0}" != "1" ]]; then
    [[ -n "${DESTINATION_CONTAINER_NAME:-}" ]] && docker rm -f "${DESTINATION_CONTAINER_NAME}" >/dev/null 2>&1 || true
    [[ -n "${CONTAINER_NAME:-}" ]] && docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  else
    echo "Keeping benchmark containers for inspection: ${DESTINATION_CONTAINER_NAME:-none}, ${CONTAINER_NAME:-none}" >&2
  fi
}
trap cleanup EXIT

if [[ "${LLRDC_MODE_RUN:-}" != "1" && "${LLRDC_COMPARE_MODES:-1}" == "1" ]]; then
  mkdir -p "${ARTIFACT_DIR}"
  calibration_sessions="${LLRDC_CALIBRATION_SESSIONS:-5}"
  if ((${#DEFAULT_MODES[@]} == 2)); then
    RUN_ORDER=(compat direct direct compat)
  else
    RUN_ORDER=("${DEFAULT_MODES[@]}")
  fi
  for session in $(seq 1 "${calibration_sessions}"); do
    for mode in "${RUN_ORDER[@]}"; do
      LLRDC_MODE_RUN=1 LLRDC_CAPTURE_MODE="${mode}" LLRDC_COMPARE_MODES=0 \
        LLRDC_ARTIFACT_DIR="${ARTIFACT_DIR}/session-${session}/${mode}-$(date +%s%N)" "${BASH_SOURCE[0]}"
    done
  done
  python3 - "${ARTIFACT_DIR}" "${ACCELERATOR}" "${DEFAULT_MODES[@]}" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
accelerator = sys.argv[2]
modes = sys.argv[3:]
reports = {mode: [] for mode in modes}
for mode in modes:
    for path in sorted(root.glob(f"session-*/{mode}-*/latency-report.json")):
        reports[mode].append(json.load(open(path)))
    if not reports[mode]:
        raise SystemExit(f"no reports for {mode}")
summary = {"accelerator": accelerator, "modes": {}}
for mode, values in reports.items():
    p95 = sorted(v["p95Ns"] for v in values)
    print(mode, json.dumps({"runs": len(p95), "p95Ns": p95}))
for mode, values in reports.items():
    p95 = sorted(v["p95Ns"] for v in values)
    median = p95[len(p95) // 2]
    summary["modes"][mode] = {
        "runs": len(p95),
        "p95Ns": p95,
        "medianP95Ns": median,
        "committedP95CeilingNs": max(int(median * 1.20), median + 5_000_000),
    }
if "compat" in reports and "direct" in reports:
    compat = summary["modes"]["compat"]["medianP95Ns"]
    direct = summary["modes"]["direct"]["medianP95Ns"]
    if direct > compat + 16_666_667:
        raise SystemExit(f"direct p95 regressed by more than one 60Hz refresh: direct={direct} compat={compat}")
json.dump(summary, open(root / "calibration.json", "w"), indent=2)
PY
  exit 0
fi

mkdir -p "${ARTIFACT_DIR}"
SERVER_PORT="$(get_free_port)"
CONTROL_PORT="$(get_free_port)"
CONTAINER_NAME="llrdc-native-latency-${SERVER_PORT}"
CLIENT_LOG="${ARTIFACT_DIR}/client-latency.log"
WESTON_LOG="${ARTIFACT_DIR}/weston-bench.log"
REPORT_TXT="${ARTIFACT_DIR}/latency-report.txt"
RESULTS_JSONL="${ARTIFACT_DIR}/latency-results.jsonl"
REJECTIONS_TSV="${ARTIFACT_DIR}/latency-rejections.tsv"
CLIENT_CONFIG="${ARTIFACT_DIR}/benchmark-client-config.yaml"

if [[ -f "${ROOT_DIR}/config.yaml" ]]; then
  cp "${ROOT_DIR}/config.yaml" "${CLIENT_CONFIG}"
else
  : >"${CLIENT_CONFIG}"
fi
if ! grep -q '^dpi:' "${CLIENT_CONFIG}"; then
  printf '\ndpi: %s\n' "${CLIENT_DPI}" >>"${CLIENT_CONFIG}"
fi

echo "▶ Building..."
if [[ "${LLRDC_SKIP_BUILD:-0}" != "1" ]]; then
  if [[ "${ACCELERATOR}" == "cpu" ]]; then
    "${ROOT_DIR}/docker-build.sh" >/dev/null 2>&1
  else
    "${ROOT_DIR}/docker-build.sh" "${BUILD_FLAGS[0]}" >/dev/null 2>&1
  fi
  "${ROOT_DIR}/scripts/package-native-client.sh" >/dev/null 2>&1
fi

start_weston
start_server
wait_for_actual_encoder
start_probe
start_client

echo "▶ Stabilizing..."
sleep "${LLRDC_STABILIZE_SECONDS:-20}"
wait_for_presented_frame 120
CURRENT_MARKER="$(read_probe_marker)"
if [[ "${CURRENT_MARKER}" =~ ^[0-9]+$ ]]; then
  # Keep control sample IDs strictly ahead of any startup/probe marker that
  # may have been generated before the measured phase.
  SAMPLE_ID="${CURRENT_MARKER}"
fi

echo "▶ Warmup (${WARMUP_COUNT})..."
for _ in $(seq 1 "${WARMUP_COUNT}"); do perform_sample 0 "${CURRENT_MARKER}"; done
echo "▶ Samples (${SAMPLE_COUNT})..."
for _ in $(seq 1 "${SAMPLE_COUNT}"); do perform_sample 1 "${CURRENT_MARKER}"; sleep 0.5; done

collect_results
LLRDC_P95_CEILING_NS="${LLRDC_P95_CEILING_NS:-0}" python3 - "${RESULTS_JSONL}" "${ARTIFACT_DIR}/latency-report.json" <<'PY'
import json, os, statistics, sys
rows = [json.loads(line)["totalNs"] for line in open(sys.argv[1]) if line.strip()]
if not rows:
    raise SystemExit("no valid latency rows")
rows.sort()
def percentile(p):
    rank = (len(rows) - 1) * p
    lo, hi = int(rank), min(len(rows) - 1, int(rank) + 1)
    if lo == hi:
        return rows[lo]
    return int(rows[lo] + (rows[hi] - rows[lo]) * (rank - lo))
report = {"count": len(rows), "minNs": rows[0], "p50Ns": percentile(.50),
          "p95Ns": percentile(.95), "p99Ns": percentile(.99), "maxNs": rows[-1],
          "meanNs": int(statistics.mean(rows))}
ceiling = int(os.environ.get("LLRDC_P95_CEILING_NS", "0"))
if ceiling and report["p95Ns"] > ceiling:
    raise SystemExit(f"p95 {report['p95Ns']} exceeds committed ceiling {ceiling}")
json.dump(report, open(sys.argv[2], "w"), indent=2)
print(json.dumps(report))
PY
echo "✅ Done. Report: ${REPORT_TXT}"
printf "\nFinal Results Summary:\n"
cat "${REPORT_TXT}"
