package common

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func getCertsDir() string {
	if dir := os.Getenv("CERTS_DIR"); dir != "" {
		return dir
	}
	return "./certs"
}

func GenerateSelfSignedCert() (tls.Certificate, string, error) {
	certsDir := getCertsDir()
	certPath := filepath.Join(certsDir, "cert.pem")
	keyPath := filepath.Join(certsDir, "key.pem")

	// Attempt to load existing certificate if both files exist
	if _, errCert := os.Stat(certPath); errCert == nil {
		if _, errKey := os.Stat(keyPath); errKey == nil {
			loadedCert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil && len(loadedCert.Certificate) > 0 {
				x509Cert, err := x509.ParseCertificate(loadedCert.Certificate[0])
				if err == nil {
					now := time.Now()
					// Check if certificate is currently valid and not expiring in the next 5 minutes
					if now.After(x509Cert.NotBefore) && now.Before(x509Cert.NotAfter.Add(-5*time.Minute)) {
						hash := sha256.Sum256(loadedCert.Certificate[0])
						fingerprint := base64.StdEncoding.EncodeToString(hash[:])
						log.Printf("Loaded existing valid certificate from %s (valid until %v)", certPath, x509Cert.NotAfter)
						return loadedCert, fingerprint, nil
					}
					log.Printf("Existing certificate at %s is expired or about to expire (valid until %v). Regenerating...", certPath, x509Cert.NotAfter)
				} else {
					log.Printf("Failed to parse existing certificate at %s: %v. Regenerating...", certPath, err)
				}
			} else {
				log.Printf("Failed to load existing X509 key pair from %s and %s: %v. Regenerating...", certPath, keyPath, err)
			}
		}
	}

	// Generate new self-signed certificate
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	notBefore := time.Now()
	// WebTransport certs must be valid for <= 14 days.
	notAfter := notBefore.Add(13 * 24 * time.Hour)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"LLrdc"},
			CommonName:   "LLrdc Self-Signed",
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// Explicitly add common local names and IPs to SAN for better browser compatibility (Safari)
		DNSNames:    []string{"localhost", "llrdc.local"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}

	// Add all local interface IPs to the certificate
	if ifaces, err := net.Interfaces(); err == nil {
		for _, i := range ifaces {
			if addrs, err := i.Addrs(); err == nil {
				for _, addr := range addrs {
					var ip net.IP
					switch v := addr.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					}
					if ip != nil && !ip.IsLoopback() {
						template.IPAddresses = append(template.IPAddresses, ip)
					}
				}
			}
		}
	}

	// Add hostname if available
	if hostname, err := os.Hostname(); err == nil {
		template.DNSNames = append(template.DNSNames, hostname)
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, "", err
	}

	// Try to save to files
	if errDir := os.MkdirAll(certsDir, 0755); errDir != nil {
		log.Printf("Failed to create certs directory %s: %v. Proceeding with memory-only cert.", certsDir, errDir)
	} else {
		certPemBlock := &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: derBytes,
		}
		certPemBytes := pem.EncodeToMemory(certPemBlock)

		keyBytes, errKey := x509.MarshalECPrivateKey(priv)
		if errKey == nil {
			keyPemBlock := &pem.Block{
				Type:  "EC PRIVATE KEY",
				Bytes: keyBytes,
			}
			keyPemBytes := pem.EncodeToMemory(keyPemBlock)

			errCertWrite := os.WriteFile(certPath, certPemBytes, 0644)
			errKeyWrite := os.WriteFile(keyPath, keyPemBytes, 0600)
			if errCertWrite == nil && errKeyWrite == nil {
				log.Printf("Successfully saved generated certificate and key to %s", certsDir)
			} else {
				log.Printf("Failed to write certificate or key file: certErr=%v, keyErr=%v", errCertWrite, errKeyWrite)
			}
		} else {
			log.Printf("Failed to marshal EC private key: %v", errKey)
		}
	}

	// Calculate SHA-256 hash of the DER bytes for WebTransport pinning
	hash := sha256.Sum256(derBytes)
	fingerprint := base64.StdEncoding.EncodeToString(hash[:])

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}

	return cert, fingerprint, nil
}
