package common

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/quic-go/webtransport-go"
)

type WebTransportSession struct {
	session       *webtransport.Session
	videoStream   webtransport.SendStream
	controlStream webtransport.Stream
	mu            sync.Mutex
}

var (
	wtSessions      = make(map[*webtransport.Session]*WebTransportSession)
	wtSessionsMutex sync.RWMutex

	WebTransportFingerprint string
	wtServer                *webtransport.Server
	
	// Store the DER bytes to serve them for manual trust (Safari)
	certDerBytes []byte

	MessageHandler func(msg map[string]interface{}, writeJSON func(interface{}) error)
)

func InitWebTransport(addr string) {
	cert, fingerprint, err := GenerateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate WebTransport certificate: %v", err)
	}
	WebTransportFingerprint = fingerprint
	if len(cert.Certificate) > 0 {
		certDerBytes = cert.Certificate[0]
	}
	log.Printf("WebTransport self-signed certificate generated. Fingerprint: %s", fingerprint)

	wtServer = &webtransport.Server{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Shared handler for both TCP TLS and HTTP/3
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Detect if this is an HTTP/3 request
		_, isH3 := w.(http3.Hijacker)

		// Always set Alt-Svc to advertise H3
		w.Header().Set("Alt-Svc", fmt.Sprintf(`h3="%s"`, addr))

		if r.URL.Path == "/webtransport" {
			if !isH3 {
				log.Printf("TCP: WebTransport upgrade rejected (requires UDP/H3)")
				http.Error(w, "WebTransport requires HTTP/3 (UDP). Please use the H3 port.", http.StatusBadRequest)
				return
			}

			log.Printf("H3: WebTransport upgrade request from %s", r.RemoteAddr)
			session, err := wtServer.Upgrade(w, r)
			if err != nil {
				log.Printf("H3: WebTransport upgrade failed: %v", err)
				return
			}
			go handleWebTransportSession(session)
			return
		}

		// Endpoint to download the certificate for manual trust (Safari)
		if r.URL.Path == "/cert" {
			w.Header().Set("Content-Type", "application/x-x509-ca-cert")
			w.Header().Set("Content-Disposition", `attachment; filename="llrdc.crt"`)
			w.Write(certDerBytes)
			return
		}

		if r.URL.Path != "/favicon.ico" {
			proto := "TCP"
			if isH3 {
				proto = "H3"
			}
			log.Printf("%s: %s %s from %s", proto, r.Method, r.URL.Path, r.RemoteAddr)
		}

		http.DefaultServeMux.ServeHTTP(w, r)
	})

	wtServer.H3 = http3.Server{
		Addr:    addr,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h3"},
		},
		QUICConfig: &quic.Config{
			MaxIdleTimeout:  30 * time.Second,
			EnableDatagrams: true,
		},
		EnableDatagrams: true,
	}

	// Start standard HTTPS (TCP)
	tcpTlsServer := &http.Server{
		Addr:    addr,
		Handler: handler,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			NextProtos:   []string{"h2", "http/1.1"},
		},
	}

	go func() {
		log.Printf("WebTransport (HTTPS/TCP) listening on TCP %s", addr)
		if err := tcpTlsServer.ListenAndServeTLS("", ""); err != nil {
			log.Printf("TCP TLS server failed: %v", err)
		}
	}()

	// Start WebTransport / HTTP/3 (UDP)
	go func() {
		log.Printf("WebTransport (HTTP/3) listening on UDP %s", addr)
		if err := wtServer.ListenAndServe(); err != nil {
			log.Printf("WebTransport server failed: %v", err)
		}
	}()
}

func handleWebTransportSession(session *webtransport.Session) {
	log.Printf("WebTransport session established: %v", session.RemoteAddr())
	if OnPeerConnected != nil {
		OnPeerConnected()
	}

	// Create a unidirectional stream for video
	videoStream, err := session.OpenUniStream()
	if err != nil {
		log.Printf("Failed to open WebTransport video stream: %v", err)
		session.CloseWithError(0, "failed to open video stream")
		return
	}

	wtSession := &WebTransportSession{
		session:     session,
		videoStream: videoStream,
	}

	wtSessionsMutex.Lock()
	wtSessions[session] = wtSession
	wtSessionsMutex.Unlock()

	defer func() {
		wtSessionsMutex.Lock()
		delete(wtSessions, session)
		wtSessionsMutex.Unlock()
		log.Printf("WebTransport session closed: %v", session.RemoteAddr())
	}()

	// Accept bidirectional streams for control
	for {
		stream, err := session.AcceptStream(session.Context())
		if err != nil {
			break
		}
		wtSession.mu.Lock()
		wtSession.controlStream = stream
		wtSession.mu.Unlock()
		go handleControlStream(wtSession, stream)
	}
}

func handleControlStream(wtSession *WebTransportSession, stream webtransport.Stream) {
	defer stream.Close()
	log.Printf("WebTransport control stream opened")

	decoder := json.NewDecoder(stream)
	encoder := json.NewEncoder(stream)

	writeJSON := func(v interface{}) error {
		wtSession.mu.Lock()
		defer wtSession.mu.Unlock()
		return encoder.Encode(v)
	}

	for {
		var msg map[string]interface{}
		if err := decoder.Decode(&msg); err != nil {
			break
		}
		if MessageHandler != nil {
			MessageHandler(msg, writeJSON)
		}
	}
}

func BroadcastWebTransportJSON(v interface{}) {
	wtSessionsMutex.RLock()
	defer wtSessionsMutex.RUnlock()

	for _, s := range wtSessions {
		s.mu.Lock()
		if s.controlStream != nil {
			_ = json.NewEncoder(s.controlStream).Encode(v)
		}
		s.mu.Unlock()
	}
}

func CloseAllWebTransportSessions() {
	wtSessionsMutex.Lock()
	defer wtSessionsMutex.Unlock()

	for session := range wtSessions {
		_ = session.CloseWithError(0, "server shutdown/restart")
	}
	wtSessions = make(map[*webtransport.Session]*WebTransportSession)
}

func WriteWebTransportFrame(frame []byte, streamID uint32, captureTime time.Time) {
	wtSessionsMutex.RLock()
	defer wtSessionsMutex.RUnlock()

	if len(wtSessions) == 0 {
		return
	}

	timestamp := float64(captureTime.UnixNano()) / float64(time.Millisecond)
	packetLen := uint32(9 + len(frame))
	header := make([]byte, 13) // 4 (length) + 1 (type) + 8 (timestamp)
	binary.BigEndian.PutUint32(header[0:], packetLen)
	header[4] = 1 // Video Type
	binary.BigEndian.PutUint64(header[5:], math.Float64bits(timestamp))

	packet := append(header, frame...)

	for _, s := range wtSessions {
		s.mu.Lock()
		_, _ = s.videoStream.Write(packet)
		s.mu.Unlock()
	}
}
