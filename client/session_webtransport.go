package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/url"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/webtransport-go"
)

func (s *Session) connectWebTransport(connectionID uint64, serverURL string, port int, fingerprint string) error {
	u, err := url.Parse(serverURL)
	if err != nil {
		return err
	}

	wtURL := fmt.Sprintf("https://%s:%d/webtransport", u.Hostname(), port)

	expectedHash, err := base64.StdEncoding.DecodeString(fingerprint)
	if err != nil {
		return fmt.Errorf("invalid fingerprint: %w", err)
	}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("no certificate provided")
			}
			hash := sha256.Sum256(rawCerts[0])
			if !bytes.Equal(hash[:], expectedHash) {
				return fmt.Errorf("certificate fingerprint mismatch")
			}
			return nil
		},
	}

	dialer := &webtransport.Dialer{
		TLSClientConfig: tlsConfig,
		QUICConfig: &quic.Config{
			KeepAlivePeriod: 10 * time.Second,
			EnableDatagrams: true,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Connecting to WebTransport at %s...", wtURL)
	rsp, wtSession, err := dialer.Dial(ctx, wtURL, nil)
	if err != nil {
		return fmt.Errorf("webtransport dial failed: %w", err)
	}
	if rsp.StatusCode != 200 {
		_ = wtSession.CloseWithError(0, "")
		return fmt.Errorf("webtransport connection rejected with status %d", rsp.StatusCode)
	}

	log.Println("WebTransport connected successfully. Opening control stream...")

	controlStream, err := wtSession.OpenStreamSync(context.Background())
	if err != nil {
		_ = wtSession.CloseWithError(0, "")
		return fmt.Errorf("open webtransport control stream: %w", err)
	}

	s.mu.Lock()
	if s.connectionID != connectionID {
		s.mu.Unlock()
		_ = controlStream.Close()
		_ = wtSession.CloseWithError(0, "")
		return nil
	}
	s.wtSession = wtSession
	s.wtControl = controlStream
	s.state.WebTransportConnected = true
	s.mu.Unlock()

	s.emit(EventStateChanged, map[string]any{
		"webtransportConnected": true,
	})

	go s.readWebTransportControlLoop(connectionID, controlStream)
	go s.acceptWebTransportStreams(connectionID, wtSession)

	return nil
}

func (s *Session) readWebTransportControlLoop(connectionID uint64, stream webtransport.Stream) {
	scanner := bufio.NewScanner(stream)
	// WebTransport uses JSON per line for control
	for scanner.Scan() {
		raw := scanner.Bytes()

		var msg map[string]any
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.setError(err)
			continue
		}

		s.mu.Lock()
		s.stats.SignalingMessages++
		s.state.LastMessageAt = time.Now()
		s.mu.Unlock()

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "pong":
			if ts, ok := numberToInt64(msg["ts"]); ok {
				if serverTs, ok2 := numberToInt64(msg["serverTs"]); ok2 {
					now := BenchmarkClockNowMs()
					rtt := now - ts
					s.mu.Lock()
					s.state.Ping = rtt
					// Refine offset: server sampled its clock at ts + rtt/2
					s.state.ServerTimeOffset = (ts + rtt/2) - serverTs
					s.mu.Unlock()
				}
			}
		case "config":
			s.mu.Lock()
			s.state.LastConfig = cloneMap(msg)
			if codec, ok := msg["videoCodec"].(string); ok {
				s.state.VideoCodec = codec
			}
			if width, ok := numberToInt(msg["screenWidth"]); ok {
				s.state.ServerScreenWidth = width
			}
			if height, ok := numberToInt(msg["screenHeight"]); ok {
				s.state.ServerScreenHeight = height
			}
			s.mu.Unlock()
			s.emit(EventConfig, cloneMap(msg))
		case "stats":
			s.mu.Lock()
			s.state.LastStats = cloneMap(msg)
			s.mu.Unlock()
			s.emit(EventStats, cloneMap(msg))
		case "reconnect_hint":
			s.emit(EventReconnectRequest, nil)
		default:
			s.emit(EventStateChanged, cloneMap(msg))
		}
	}

	if err := scanner.Err(); err != nil {
		s.setError(fmt.Errorf("webtransport control read error: %w", err))
		go func() {
			_ = s.disconnectIfCurrent(connectionID)
		}()
	}
}

func (s *Session) acceptWebTransportStreams(connectionID uint64, wtSession *webtransport.Session) {
	for {
		stream, err := wtSession.AcceptUniStream(context.Background())
		if err != nil {
			s.mu.RLock()
			current := s.connectionID == connectionID
			s.mu.RUnlock()
			if current {
				s.setError(fmt.Errorf("accept webtransport uni stream: %w", err))
				go func() {
					_ = s.disconnectIfCurrent(connectionID)
				}()
			}
			return
		}

		// Each uni stream handles one media track (currently just video)
		go s.readWebTransportMediaStream(connectionID, stream)
	}
}

func (s *Session) readWebTransportMediaStream(connectionID uint64, stream webtransport.ReceiveStream) {
	headerBuf := make([]byte, 4)
	for {
		_, err := io.ReadFull(stream, headerBuf)
		if err != nil {
			return
		}

		packetLen := binary.BigEndian.Uint32(headerBuf)
		if packetLen < 9 {
			continue // Invalid packet
		}

		payloadLen := packetLen
		payloadBuf := make([]byte, payloadLen)
		_, err = io.ReadFull(stream, payloadBuf)
		if err != nil {
			return
		}

		streamType := payloadBuf[0]
		if streamType == 1 { // Video
			timestampMs := math.Float64frombits(binary.BigEndian.Uint64(payloadBuf[1:9]))
			packetTimestamp := int64(timestampMs)
			chunkData := payloadBuf[9:]

			s.mu.RLock()
			codec := s.state.VideoCodec
			s.mu.RUnlock()

			receiveAt := BenchmarkClockNowMs()

			s.mu.Lock()
			now := time.Now()
			s.stats.VideoPackets++
			s.stats.VideoFrames++
			s.stats.VideoBytes += uint64(len(chunkData))
			s.state.LastVideoPacketAt = now
			s.state.LastVideoFrameAt = s.state.LastVideoPacketAt
			s.recordVideoByteSampleLocked(now, len(chunkData))
			s.mu.Unlock()

			var renderErr error
			if tr, ok := s.renderer.(TimedVideoFrameHandler); ok {
				renderErr = tr.HandleVideoFrameWithTiming(
					codec,
					chunkData,
					packetTimestamp,
					0,         // sequence number not used in WT uni-streams yet
					0,         // queue time not used
					0,         // remote packet time not used
					receiveAt, // read at
					receiveAt, // receive at
				)
			} else {
				renderErr = s.renderer.HandleVideoFrame(codec, chunkData, packetTimestamp)
			}

			if renderErr != nil {
				s.setError(renderErr)
				continue
			}

			s.emit(EventFrame, map[string]any{
				"codec":           codec,
				"packetTimestamp": packetTimestamp,
				"size":            len(chunkData),
				"transport":       "webtransport",
			})
		}
	}
}
