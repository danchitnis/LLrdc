#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_ROOT="${PACKAGE_ROOT:-${ROOT_DIR}/dist}"
PACKAGE_NAME="${PACKAGE_NAME:-llrdc-client-linux-amd64}"
CLIENT_BIN="${PACKAGE_ROOT}/${PACKAGE_NAME}/bin/llrdc-client"

MODE="${LLRDC_LATENCY_MODE:-control}"
FPS="${LLRDC_TARGET_FPS:-60}"
WINDOW_TITLE="${LLRDC_CLIENT_TITLE:-LLrdc Native Latency Bench}"
WINDOW_WIDTH="${LLRDC_CLIENT_WIDTH:-1280}"
WINDOW_HEIGHT="${LLRDC_CLIENT_HEIGHT:-720}"
WARMUP_COUNT="${LLRDC_WARMUP_COUNT:-3}"
SAMPLE_COUNT="${LLRDC_SAMPLE_COUNT:-5}"
ARTIFACT_DIR="${LLRDC_ARTIFACT_DIR:-/tmp/llrdc-native-latency}"
WESTON_BACKEND="${LLRDC_WESTON_BACKEND:-wayland}"
WESTON_SOCKET="${LLRDC_WESTON_SOCKET:-llrdc-bench-$$}"
VIDEO_CODEC="${LLRDC_VIDEO_CODEC:-libvpx}"

MEASURED_MARKERS=()

get_free_port() {
  local port=0
  while [[ "${port}" -eq 0 || -n "$(ss -Htan "( sport = :${port} )")" ]]; do
    port=$((RANDOM % 1000 + 8000))
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
read_server_time() { curl -fsS "http://127.0.0.1:${SERVER_PORT}/timez"; }
read_server_trace() { curl -fsS "http://127.0.0.1:${SERVER_PORT}/latencyz?marker=$1"; }

host_monotonic_ms() {
  python3 - <<'PY'
import time
print(time.clock_gettime_ns(time.CLOCK_MONOTONIC) // 1_000_000)
PY
}

calibrate_clock_offset() {
  local tmp
  tmp="$(mktemp)"
  for _ in $(seq 1 10); do
    local before response server after rtt midpoint offset
    before="$(host_monotonic_ms)"
    response="$(read_server_time)"
    after="$(host_monotonic_ms)"
    server="$(printf '%s' "${response}" | jq -r '.serverTimeMs // 0')"
    if [[ ! "${server}" =~ ^[0-9]+$ ]] || (( server <= 0 )); then
      continue
    fi
    rtt=$((after - before))
    midpoint=$(((before + after) / 2))
    offset=$((midpoint - server))
    printf '%s\t%s\n' "${rtt}" "${offset}" >>"${tmp}"
    sleep 0.05
  done

  if [[ ! -s "${tmp}" ]]; then
    rm -f "${tmp}"
    printf '0\t999999\t0\n'
    return 0
  fi

  local selected offsets median jitter count min_offset max_offset
  selected="$(sort -n "${tmp}" | head -n 5)"
  offsets="$(printf '%s\n' "${selected}" | awk '{print $2}' | sort -n)"
  count="$(printf '%s\n' "${offsets}" | awk 'NF { count++ } END { print count + 0 }')"
  median="$(printf '%s\n' "${offsets}" | awk -v target="$(((count + 1) / 2))" 'NF && ++i == target { print $1 }')"
  min_offset="$(printf '%s\n' "${offsets}" | awk 'NF { print $1; exit }')"
  max_offset="$(printf '%s\n' "${offsets}" | awk 'NF { value = $1 } END { print value + 0 }')"
  jitter=$((max_offset - min_offset))
  rm -f "${tmp}"
  printf '%s\t%s\t%s\n' "${median:-0}" "${jitter:-999999}" "${count}"
}

wait_for_client_ready() {
  for i in {1..45}; do
    local ready=""
    ready="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/readyz" 2>/dev/null || true)"
    if printf '%s' "${ready}" | jq -e '.webrtcConnected == true and .windowVisible == true and .windowHasSurface == true and .renderLoopStarted == true' >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  return 1
}

start_weston() {
  echo "▶ Launching Weston bench..."
  local weston_cmd=(weston "--backend=${WESTON_BACKEND}" "--socket=${WESTON_SOCKET}" "--width=${WINDOW_WIDTH}" "--height=${WINDOW_HEIGHT}" "--idle-time=0" "--log=${WESTON_LOG}")
  setsid "${weston_cmd[@]}" >/dev/null 2>&1 &
  WESTON_PID=$!
  local socket_path="/tmp/llrdc-run/${WESTON_SOCKET}"
  if [[ "${WESTON_BACKEND}" == "wayland" ]]; then socket_path="${XDG_RUNTIME_DIR}/${WESTON_SOCKET}"; fi
  for _ in {1..20}; do
    if [[ -S "${socket_path}" ]]; then return 0; fi
    sleep 0.5
  done
  exit 1
}

start_server() {
  echo "▶ Starting server in Docker..."
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
  docker run -d --name "${CONTAINER_NAME}" \
    --network host \
    -e VIDEO_CODEC="${VIDEO_CODEC}" \
    -e VBR=false \
    -e PORT="${SERVER_PORT}" \
    -e FPS="${FPS}" \
    -e HDPI=100 \
    -e LLRDC_MINIMAL_WAYLAND=1 \
    -e RESOLUTION="${WINDOW_WIDTH}x${WINDOW_HEIGHT}" \
    -e TEST_PATTERN="${TEST_PATTERN:-}" \
    -e DAMAGE_TRACKING="${DAMAGE_TRACKING:-}" \
    -e CAPTURE_MODE="${CAPTURE_MODE:-}" \
    -e WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-}" \
    -e WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-}" \
    danchitnis/llrdc:latest \
    /app/llrdc --port "${SERVER_PORT}" --res "${WINDOW_HEIGHT}" >/dev/null
  
  for _ in {1..40}; do
    if curl -fsS "http://127.0.0.1:${SERVER_PORT}/readyz" >/dev/null 2>&1; then return 0; fi
    sleep 0.25
  done
  exit 1
}

start_probe() {
  echo "▶ Launching remote latency probe..."
  docker exec -u remote -d "${CONTAINER_NAME}" bash -lc \
    "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1"
  sleep 5
}

start_client() {
  echo "▶ Launching native client..."
  setsid env \
    WAYLAND_DISPLAY="${WESTON_SOCKET}" \
    "${CLIENT_BIN}" \
    --server "http://127.0.0.1:${SERVER_PORT}" \
    --control-addr "127.0.0.1:${CONTROL_PORT}" \
    --title "${WINDOW_TITLE}" \
    --width "${WINDOW_WIDTH}" \
    --height "${WINDOW_HEIGHT}" \
    --auto-start \
    --latency-probe >"${CLIENT_LOG}" 2>&1 &
  CLIENT_PID=$!
  wait_for_client_ready
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
  for _ in $(seq 1 "${timeout}"); do
    local trace packet_timestamp packet_sequence
    trace="$(read_server_trace "${marker}" 2>/dev/null || true)"
    packet_timestamp="$(printf '%s' "${trace}" | jq -r '.firstPacketTimestamp // 0' 2>/dev/null || echo 0)"
    packet_sequence="$(printf '%s' "${trace}" | jq -r '.firstPacketSequenceNumber // 0' 2>/dev/null || echo 0)"
    if [[ "${packet_timestamp}" =~ ^[0-9]+$ ]] && [[ "${packet_sequence}" =~ ^[0-9]+$ ]] && (( packet_timestamp > 0 )) && (( packet_sequence > 0 )); then
      printf '%s\t%s\n' "${packet_timestamp}" "${packet_sequence}"
      return 0
    fi
    sleep 0.1
  done
  return 1
}

wait_for_client_frame_identity() {
  local packet_timestamp="$1"
  local packet_sequence="$2"
  local timeout="$3"
  for _ in $(seq 1 "${timeout}"); do
    if read_client_state | jq -e --argjson ts "${packet_timestamp}" --argjson seq "${packet_sequence}" '
      any((.recentLatencySamples // [])[];
        (.packetTimestamp // 0) == $ts and
        (.firstPacketSequenceNumber // 0) == $seq
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
  prior_presentation="$(read_latest_client_sample | jq -r '.presentationAt // 0')"
  
  # 1. Reset to BLACK (top-left)
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"x":0.1,"y":0.1}' "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null
  wait_for_latest_brightness "black" "${prior_presentation}" 40
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

  # 3. TIMED TRIGGER (The benchmark event)
  curl -fsS -X POST -H "Content-Type: application/json" -d '{"x":0.5,"y":0.5}' "http://127.0.0.1:${CONTROL_PORT}/input/mousemove" >/dev/null

  local next_marker
  if ! next_marker="$(wait_for_marker_increment "${previous_marker}" 40)"; then
    echo "❌ Probe marker did not advance" >&2
    exit 1
  fi

  # 4. Wait for the exact frame that matches the server's traced first RTP packet.
  local identity packet_timestamp packet_sequence
  if ! identity="$(wait_for_server_trace_identity "${next_marker}" 40)"; then
    echo "❌ Failed to get traced packet identity for marker ${next_marker}" >&2
    exit 1
  fi
  packet_timestamp="$(printf '%s' "${identity}" | cut -f1)"
  packet_sequence="$(printf '%s' "${identity}" | cut -f2)"
  if ! wait_for_client_frame_identity "${packet_timestamp}" "${packet_sequence}" 40; then
    echo "❌ Failed to observe traced frame for marker ${next_marker} (ts=${packet_timestamp} seq=${packet_sequence})" >&2
    exit 1
  fi

  if [[ "${record_result}" == "1" ]]; then
    MEASURED_MARKERS+=("${next_marker}")
  fi
  CURRENT_MARKER="${next_marker}"
}

collect_results() {
  local client_state
  client_state="$(read_client_state)"
  cat >"${RESULTS_TSV}" <<'EOF'
marker	control_req	render	post_draw_capture_encode	server_dispatch	packetize	send_call	send_to_socket	socket_to_decrypt_raw	socket_to_decrypt_corr	srtp_queue	app_read	assemble	client	sender_flush	total
EOF
  {
    echo "---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------"
    echo " Native Linux Latency Bench (Control API -> Native Present, Monotonic Clock)"
    echo "---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------"
    echo " Scenario             : TEST_PATTERN=${TEST_PATTERN:-0} DAMAGE_TRACKING=${DAMAGE_TRACKING:-default} CAPTURE_MODE=${CAPTURE_MODE:-default} VIDEO_CODEC=${VIDEO_CODEC}"
    echo " Clock offset before: ${CLOCK_OFFSET_BEFORE_MS} ms (jitter ${CLOCK_OFFSET_BEFORE_JITTER_MS} ms)"
    echo " Clock offset after : ${CLOCK_OFFSET_AFTER_MS} ms (jitter ${CLOCK_OFFSET_AFTER_JITTER_MS} ms)"
    echo " Clock offset used  : ${CLOCK_OFFSET_ESTIMATE_MS} ms"
    if (( CLOCK_OFFSET_UNSTABLE != 0 )); then
      echo " Timing stability   : unstable clock offset calibration"
    fi
    echo "---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------"
    echo " Marker | Ctrl->Req | Render | PostDraw+Encode | Dispatch | Packetize | SendCall | Send->Sock | Sock->Decrypt | Sock->Decrypt* | SRTPQ | AppRead | Assemble | Client | SenderFlush | Total E2E | Present"
    echo "---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------"
  } | tee "${REPORT_TXT}"
  for marker in "${MEASURED_MARKERS[@]}"; do
    local server_trace sample
    server_trace="$(curl -fsS "http://127.0.0.1:${SERVER_PORT}/latencyz?marker=${marker}")"
    if [[ -z "${server_trace}" || "${server_trace}" == "null" ]]; then
      echo "❌ Missing server trace for marker ${marker}" >&2
      exit 1
    fi
    
    local s_t0 s_t1 s_t2 s_t2a s_t2b s_t3 s_t4 s_t5 s_t5a s_t6 s_pkt_ts s_pkt_seq
    s_t0=$(printf '%s' "${server_trace}" | jq -r '.serverTimeMs // 0')
    s_t1=$(printf '%s' "${server_trace}" | jq -r '.requestedAtMs // 0')
    s_t2=$(printf '%s' "${server_trace}" | jq -r '.drawnAtMs // 0')
    s_t2a=$(printf '%s' "${server_trace}" | jq -r '.firstEncodedFrameParsedAtMs // (.frameSendStartAtMs // .firstFrameBroadcastAtMs // 0)')
    s_t2b=$(printf '%s' "${server_trace}" | jq -r '.firstFrameDispatchAtMs // (.frameSendStartAtMs // .firstFrameBroadcastAtMs // 0)')
    s_t3=$(printf '%s' "${server_trace}" | jq -r '.frameSendStartAtMs // (.firstFrameBroadcastAtMs // 0)')
    s_t4=$(printf '%s' "${server_trace}" | jq -r '.firstPacketWriteAttemptAtMs // (.frameSendStartAtMs // .firstFrameBroadcastAtMs // 0)')
    s_t5=$(printf '%s' "${server_trace}" | jq -r '.firstPacketWriteReturnAtMs // (.firstPacketWrittenAtMs // .firstFrameBroadcastAtMs // 0)')
    s_t5a=$(printf '%s' "${server_trace}" | jq -r '.firstPacketSocketWriteAtMs // (.firstPacketWriteReturnAtMs // .firstPacketWrittenAtMs // .firstFrameBroadcastAtMs // 0)')
    s_t6=$(printf '%s' "${server_trace}" | jq -r '.lastPacketWrittenAtMs // (.firstPacketWriteReturnAtMs // .firstPacketWrittenAtMs // .firstFrameBroadcastAtMs // 0)')
    s_pkt_ts=$(printf '%s' "${server_trace}" | jq -r '.firstPacketTimestamp // 0')
    s_pkt_seq=$(printf '%s' "${server_trace}" | jq -r '.firstPacketSequenceNumber // 0')

    sample="$(printf '%s' "${client_state}" | jq -c --argjson ts "${s_pkt_ts}" --argjson seq "${s_pkt_seq}" '
      [(.recentLatencySamples // [])[]
        | select((.packetTimestamp // 0) == $ts)
        | select((.firstPacketSequenceNumber // 0) == $seq)
      ] | sort_by(.presentationAt) | .[0] // empty
    ')"
    if [[ -z "${sample}" ]]; then
      echo "❌ Missing client sample for marker ${marker} (ts=${s_pkt_ts} seq=${s_pkt_seq})" >&2
      exit 1
    fi
    
    local c_dec c_remote c_pkt c_rec c_pre c_pkt_ts c_pkt_seq
    c_dec=$(printf '%s' "${sample}" | jq -r 'if (.firstDecryptedPacketQueuedAt // 0) > 0 then .firstDecryptedPacketQueuedAt else (if (.firstRemotePacketAt // 0) > 0 then .firstRemotePacketAt else (if (.firstPacketReadAt // 0) > 0 then .firstPacketReadAt else (.receiveAt // 0) end) end) end')
    c_remote=$(printf '%s' "${sample}" | jq -r 'if (.firstRemotePacketAt // 0) > 0 then .firstRemotePacketAt else (if (.firstPacketReadAt // 0) > 0 then .firstPacketReadAt else (.receiveAt // 0) end) end')
    c_pkt=$(printf '%s' "${sample}" | jq -r 'if (.firstPacketReadAt // 0) > 0 then .firstPacketReadAt else (.receiveAt // 0) end')
    c_rec=$(printf '%s' "${sample}" | jq -r '.receiveAt // 0')
    c_pre=$(printf '%s' "${sample}" | jq -r 'if (.compositorPresentedAt // 0) > 0 then .compositorPresentedAt else .presentationAt end')
    c_pkt_ts=$(printf '%s' "${sample}" | jq -r '.packetTimestamp // 0')
    c_pkt_seq=$(printf '%s' "${sample}" | jq -r '.firstPacketSequenceNumber // 0')

    if (( s_t0 <= 0 || s_t1 <= 0 || s_t2 <= 0 || s_t2a <= 0 || s_t2b <= 0 || s_t3 <= 0 || s_t4 <= 0 || s_t5 <= 0 || s_t5a <= 0 || s_t6 <= 0 || c_dec <= 0 || c_remote <= 0 || c_pkt <= 0 || c_rec <= 0 || c_pre <= 0 )); then
      echo "❌ Invalid zero timestamp for marker ${marker}" >&2
      exit 1
    fi
    if (( s_pkt_ts > 0 && c_pkt_ts > 0 )) && (( s_pkt_ts != c_pkt_ts )); then
      echo "❌ First packet timestamp mismatch for marker ${marker}: server=${s_pkt_ts} client=${c_pkt_ts}" >&2
      exit 1
    fi
    if (( s_pkt_seq > 0 && c_pkt_seq > 0 )) && (( s_pkt_seq != c_pkt_seq )); then
      echo "❌ First packet sequence mismatch for marker ${marker}: server=${s_pkt_seq} client=${c_pkt_seq}" >&2
      exit 1
    fi
    if ! (( s_t0 <= s_t1 && s_t1 <= s_t2 && s_t2 <= s_t2a && s_t2a <= s_t2b && s_t2b <= s_t3 && s_t3 <= s_t4 && s_t4 <= s_t5 && s_t5 <= s_t5a && s_t5a <= s_t6 && c_dec <= c_remote && c_remote <= c_pkt && c_pkt <= c_rec && c_rec <= c_pre )); then
      echo "❌ Non-monotonic latency trace for marker ${marker}" >&2
      echo "    T0=${s_t0} T1=${s_t1} T2=${s_t2} T2a=${s_t2a} T2b=${s_t2b} T3=${s_t3} T4=${s_t4} T5=${s_t5} T5a=${s_t5a} T6=${s_t6} Decrypt=${c_dec} Remote=${c_remote} Pkt=${c_pkt} Rec=${c_rec} Pre=${c_pre}" >&2
      exit 1
    fi

    local control_req render post_draw_capture_encode server_dispatch packetize send_call send_to_socket socket_to_decrypt_raw socket_to_decrypt_corr srtp_queue app_read assemble client sender_flush total
    control_req=$((s_t1 - s_t0))
    render=$((s_t2 - s_t1))
    post_draw_capture_encode=$((s_t2a - s_t2))
    server_dispatch=$((s_t2b - s_t2a))
    packetize=$((s_t4 - s_t3))
    send_call=$((s_t5 - s_t4))
    send_to_socket=$((s_t5a - s_t5))
    socket_to_decrypt_raw=$((c_dec - s_t5a))
    socket_to_decrypt_corr=$((c_dec - (s_t5a + CLOCK_OFFSET_ESTIMATE_MS)))
    srtp_queue=$((c_remote - c_dec))
    app_read=$((c_pkt - c_remote))
    assemble=$((c_rec - c_pkt))
    client=$((c_pre - c_rec))
    sender_flush=$((s_t6 - s_t5))
    total=$((c_pre - s_t0))

    printf " %6s | %9d | %6d | %15d | %8d | %9d | %8d | %10d | %13d | %14d | %5d | %7d | %8d | %6d | %11d | %9d | %s\n" \
      "${marker}" "${control_req}" "${render}" "${post_draw_capture_encode}" "${server_dispatch}" "${packetize}" "${send_call}" "${send_to_socket}" "${socket_to_decrypt_raw}" "${socket_to_decrypt_corr}" "${srtp_queue}" "${app_read}" "${assemble}" "${client}" "${sender_flush}" "${total}" \
      "$(printf '%s' "${sample}" | jq -r '.presentationSource // "render_present"')" | tee -a "${REPORT_TXT}"
    
    printf "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n" "${marker}" "${control_req}" "${render}" "${post_draw_capture_encode}" "${server_dispatch}" "${packetize}" "${send_call}" "${send_to_socket}" "${socket_to_decrypt_raw}" "${socket_to_decrypt_corr}" "${srtp_queue}" "${app_read}" "${assemble}" "${client}" "${sender_flush}" "${total}" >>"${RESULTS_TSV}"
  done
}

cleanup() {
  [[ -n "${CLIENT_PID:-}" ]] && kill_process_group "${CLIENT_PID}"
  [[ -n "${WESTON_PID:-}" ]] && kill_process_group "${WESTON_PID}"
  docker rm -f "${CONTAINER_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "${ARTIFACT_DIR}"
SERVER_PORT="$(get_free_port)"
CONTROL_PORT="$(get_free_port)"
CONTAINER_NAME="llrdc-native-latency-${SERVER_PORT}"
CLIENT_LOG="${ARTIFACT_DIR}/client-latency.log"
WESTON_LOG="${ARTIFACT_DIR}/weston-bench.log"
RESULTS_TSV="${ARTIFACT_DIR}/latency-results.tsv"
REPORT_TXT="${ARTIFACT_DIR}/latency-report.txt"

echo "▶ Building..."
"${ROOT_DIR}/docker-build.sh" >/dev/null 2>&1
"${ROOT_DIR}/scripts/package-native-client.sh" >/dev/null 2>&1

start_weston
start_server
CLOCK_BEFORE_RAW="$(calibrate_clock_offset)"
CLOCK_OFFSET_BEFORE_MS="$(printf '%s' "${CLOCK_BEFORE_RAW}" | cut -f1)"
CLOCK_OFFSET_BEFORE_JITTER_MS="$(printf '%s' "${CLOCK_BEFORE_RAW}" | cut -f2)"
start_probe
start_client

echo "▶ Stabilizing..."
sleep 10
CURRENT_MARKER="$(read_probe_marker)"

echo "▶ Warmup (${WARMUP_COUNT})..."
for _ in $(seq 1 "${WARMUP_COUNT}"); do perform_sample 0 "${CURRENT_MARKER}"; done
echo "▶ Samples (${SAMPLE_COUNT})..."
for _ in $(seq 1 "${SAMPLE_COUNT}"); do perform_sample 1 "${CURRENT_MARKER}"; sleep 0.5; done

CLOCK_AFTER_RAW="$(calibrate_clock_offset)"
CLOCK_OFFSET_AFTER_MS="$(printf '%s' "${CLOCK_AFTER_RAW}" | cut -f1)"
CLOCK_OFFSET_AFTER_JITTER_MS="$(printf '%s' "${CLOCK_AFTER_RAW}" | cut -f2)"
CLOCK_OFFSET_ESTIMATE_MS=$(((CLOCK_OFFSET_BEFORE_MS + CLOCK_OFFSET_AFTER_MS) / 2))
CLOCK_OFFSET_UNSTABLE=0
if (( CLOCK_OFFSET_BEFORE_JITTER_MS > 2 || CLOCK_OFFSET_AFTER_JITTER_MS > 2 )); then
  CLOCK_OFFSET_UNSTABLE=1
fi

collect_results
echo "✅ Done. Report: ${REPORT_TXT}"
printf "\nFinal Results Summary:\n"
cat "${REPORT_TXT}"
