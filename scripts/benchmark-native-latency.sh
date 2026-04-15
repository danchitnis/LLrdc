#!/usr/bin/env bash
set -euo pipefail

# benchmark-native-latency.sh
# Measures end-to-end latency breakdown for the native client using ydotool or Control API.

# Find an available port
get_free_port() {
    local port=$((RANDOM % 1000 + 8000))
    while lsof -Pi :$port -sTCP:LISTEN -t >/dev/null; do
        port=$((RANDOM % 1000 + 8000))
    done
    echo $port
}

SERVER_PORT=$(get_free_port)
CONTROL_PORT=$(get_free_port)
CONTAINER_NAME="llrdc-native-latency-$SERVER_PORT"
VIDEO_CODEC="vp8"
FPS=60

echo "▶ Using Server Port: $SERVER_PORT, Control Port: $CONTROL_PORT"

echo "▶ Checking dependencies..."
for cmd in jq curl docker; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        echo "❌ Error: $cmd is required but not found."
        exit 1
    fi
done

if ! command -v ydotool >/dev/null 2>&1; then
    echo "⚠️ Warning: ydotool not found. Will attempt to use Control API for input fallback."
    USE_YDOTOOL=false
else
    if ! pgrep ydotoold >/dev/null 2>&1; then
        echo "⚠️ Warning: ydotoold not running. ydotool commands will likely fail."
        USE_YDOTOOL=false
    else
        USE_YDOTOOL=true
    fi
fi

echo "▶ Building server and client..."
./docker-build.sh
./scripts/package-native-client.sh

cleanup() {
    echo ""
    echo "▶ Cleaning up..."
    if [[ -n "${CLIENT_PID:-}" ]]; then
        kill "$CLIENT_PID" 2>/dev/null || true
    fi
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

echo "▶ Starting server in Docker..."
docker run -d --name "$CONTAINER_NAME" \
    --network host \
    -e VIDEO_CODEC="$VIDEO_CODEC" \
    -e VBR=false \
    -e PORT="$SERVER_PORT" \
    -e FPS="$FPS" \
    -e WEBRTC_LOW_LATENCY="${WEBRTC_LOW_LATENCY:-}" \
    -e WEBRTC_BUFFER_SIZE="${WEBRTC_BUFFER_SIZE:-}" \
    danchitnis/llrdc:latest \
    /app/llrdc --port "$SERVER_PORT" >/dev/null

sleep 5

if ! curl -s "http://127.0.0.1:$SERVER_PORT/readyz" >/dev/null; then
    echo "❌ Error: Server failed to start on port $SERVER_PORT"
    docker logs "$CONTAINER_NAME"
    exit 1
fi

echo "▶ Launching latency probe on server..."
docker exec -u remote -d "$CONTAINER_NAME" bash -lc \
    "export XDG_RUNTIME_DIR=/tmp/llrdc-run WAYLAND_DISPLAY=wayland-0; latency_probe >/tmp/latency-probe.log 2>&1"

sleep 2

echo "▶ Launching native client..."
./dist/llrdc-client-linux-amd64/bin/llrdc-client \
    --server "http://127.0.0.1:$SERVER_PORT" \
    --control-addr "127.0.0.1:$CONTROL_PORT" \
    --auto-start \
    --latency-probe \
    --debug-cursor > client_latency.log 2>&1 &
CLIENT_PID=$!

echo "  - Waiting for client to connect..."
for i in {1..20}; do
    if curl -s "http://127.0.0.1:$CONTROL_PORT/readyz" | jq -e '.webrtcConnected == true' >/dev/null; then
        echo "  - Client connected and streaming."
        break
    fi
    if [ $i -eq 20 ]; then
        echo "❌ Error: Client timed out connecting to server."
        cat client_latency.log
        exit 1
    fi
    sleep 1
done

# Get initial marker
INITIAL_MARKER=$(docker exec "$CONTAINER_NAME" cat /tmp/llrdc-latency-probe.json | jq -r '.marker')
echo "  - Initial probe marker: $INITIAL_MARKER"

echo "▶ Performing mouse sweeps..."
for i in {1..5}; do
    echo "  - Sweep #$i..."
    if [ "$USE_YDOTOOL" = true ]; then
        # Ensure the cursor is visible and moved to the center first
        ydotool mousemove --absolute 640 360
        sleep 0.5
        ydotool mousemove 590 360
        sleep 0.2
        ydotool mousemove 690 360
        sleep 0.5
    else
        # Fallback to Control API
        curl -s -X POST -H "Content-Type: application/json" -d '{"x":0.46, "y":0.5}' "http://127.0.0.1:$CONTROL_PORT/input/mousemove" >/dev/null
        sleep 0.2
        curl -s -X POST -H "Content-Type: application/json" -d '{"x":0.54, "y":0.5}' "http://127.0.0.1:$CONTROL_PORT/input/mousemove" >/dev/null
        sleep 0.5
    fi
done

sleep 2

echo "▶ Collecting results..."
FINAL_STATE=$(docker exec "$CONTAINER_NAME" cat /tmp/llrdc-latency-probe.json)
FINAL_MARKER=$(echo "$FINAL_STATE" | jq -r '.marker')
echo "  - Final probe marker: $FINAL_MARKER"

if [ "$FINAL_MARKER" -le "$INITIAL_MARKER" ]; then
    echo "❌ FAILED: Probe marker did not increment."
    exit 1
fi

echo "▶ Synchronizing clocks via HTTP (High Precision)..."
MIN_RTT=9999
OFFSET=0
# We perform 10 rapid HTTP requests to get the lowest RTT
for i in {1..10}; do
    T1=$(date +%s%3N)
    # Fetch latest trace to get serverTimeMs
    RESP=$(curl -s "http://127.0.0.1:$SERVER_PORT/latencyz")
    STIME=$(echo "$RESP" | jq -r '.serverTimeMs // 0')
    T2=$(date +%s%3N)
    
    if [ $(echo "$STIME > 0" | bc -l) -eq 1 ]; then
        RTT=$(( T2 - T1 ))
        if [ $RTT -lt $MIN_RTT ]; then
            MIN_RTT=$RTT
            # Convert STIME to integer millisecond
            STIME_I=$(echo "$STIME" | awk '{print int($1)}')
            OFFSET=$(( STIME_I - (T1 + RTT / 2) ))
        fi
    fi
done
echo "  - Clock Offset: ${OFFSET}ms (Container - Host)"
echo "  - Sync Precision (RTT): ${MIN_RTT}ms"

echo "▶ Correlating latency traces..."
CLIENT_STATE=$(curl -s "http://127.0.0.1:$CONTROL_PORT/statez")
SAMPLES=$(echo "$CLIENT_STATE" | jq -c '.recentLatencySamples[]')

echo "--------------------------------------------------------------------------------"
echo " Latency Breakdown (ms) | Native Client"
echo "--------------------------------------------------------------------------------"
echo " Marker | In->Req | Req->Draw | Draw->Brd | Brd->Rec | Rec->Dec | Dec->Pre | E2E"
echo "--------------------------------------------------------------------------------"

for m in $(seq $((INITIAL_MARKER + 1)) "$FINAL_MARKER"); do
    SERVER_TRACE=$(curl -s "http://127.0.0.1:$SERVER_PORT/latencyz?marker=$m")
    
    if [ -z "$SERVER_TRACE" ] || [ "$SERVER_TRACE" == "null" ]; then
        continue
    fi

    REQ_MS=$(echo "$SERVER_TRACE" | jq -r '.requestedAtMs // 0')
    DRAW_MS=$(echo "$SERVER_TRACE" | jq -r '.drawnAtMs // 0')
    BRD_MS=$(echo "$SERVER_TRACE" | jq -r '.firstFrameBroadcastAtMs // 0')
    COLOR=$(echo "$SERVER_TRACE" | jq -r '.color')
    MOVE_MS=$(echo "$SERVER_TRACE" | jq -r '.firstMoveAtMs // 0')

    # Apply refined offset
    REQ_I=$(echo "$REQ_MS" | awk -v off="$OFFSET" '{print int($1 - off)}')
    DRAW_I=$(echo "$DRAW_MS" | awk -v off="$OFFSET" '{print int($1 - off)}')
    BRD_I=$(echo "$BRD_MS" | awk -v off="$OFFSET" '{print int($1 - off)}')
    MOVE_I=$(echo "$MOVE_MS" | awk -v off="$OFFSET" '{print int($1 - off)}')

    if [ "$REQ_I" -le 0 ] || [ "$BRD_I" -le 0 ]; then
        continue
    fi

    MATCHING_SAMPLE=""
    # We look for the first frame that accurately reflects the color change
    while read -r sample; do
        [ -z "$sample" ] && continue
        SREC=$(echo "$sample" | jq -r '.receiveAt')
        
        # Jitter allowance: 20ms
        if [ "$SREC" -lt $((BRD_I - 20)) ]; then continue; fi

        SB=$(echo "$sample" | jq -r '.brightness // -1')
        if [ "$COLOR" == "white" ]; then
            if [ "$SB" -ge 200 ]; then MATCHING_SAMPLE="$sample"; break; fi
        else
            if [ "$SB" -le 55 ] && [ "$SB" -ge 0 ]; then MATCHING_SAMPLE="$sample"; break; fi
        fi
    done <<< "$SAMPLES"

    if [ -n "$MATCHING_SAMPLE" ]; then
        SREC=$(echo "$MATCHING_SAMPLE" | jq -r '.receiveAt')
        SDEC=$(echo "$MATCHING_SAMPLE" | jq -r '.decodeReadyAt')
        SPRE=$(echo "$MATCHING_SAMPLE" | jq -r '.presentationAt')

        BASE_MS=$MOVE_I
        if [ "$BASE_MS" -le 0 ]; then BASE_MS=$REQ_I; fi

        IN_REQ=$((REQ_I - BASE_MS))
        REQ_DRAW=$((DRAW_I - REQ_I))
        DRAW_BRD=$((BRD_I - DRAW_I))
        BRD_REC=$((SREC - BRD_I))
        REC_DEC=$((SDEC - SREC))
        DEC_PRE=$((SPRE - SDEC))
        E2E=$((SPRE - BASE_MS))

        printf " %6d | %6d | %8d | %8d | %7d | %7d | %7d | %3d\n" \
            "$m" "$IN_REQ" "$REQ_DRAW" "$DRAW_BRD" "$BRD_REC" "$REC_DEC" "$DEC_PRE" "$E2E"
    else
        printf " %6d | (no client match found)\n" "$m"
    fi
done

echo "--------------------------------------------------------------------------------"
echo "DONE"
