package common

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	// Use a temporary directory for certs during the test to avoid messing up local certs
	tempDir, err := os.MkdirTemp("", "certs_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Set CERTS_DIR env var
	os.Setenv("CERTS_DIR", tempDir)
	defer os.Unsetenv("CERTS_DIR")

	// 1. Generate the certificate first time (it should create files)
	cert1, fingerprint1, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("first generation failed: %v", err)
	}

	certPath := filepath.Join(tempDir, "cert.pem")
	keyPath := filepath.Join(tempDir, "key.pem")

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Errorf("cert.pem was not created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Errorf("key.pem was not created")
	}

	// Parse the generated cert
	x509Cert1, err := x509.ParseCertificate(cert1.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse first certificate: %v", err)
	}

	// 2. Load the certificate the second time (it should load from files)
	cert2, fingerprint2, err := GenerateSelfSignedCert()
	if err != nil {
		t.Fatalf("second generation/load failed: %v", err)
	}

	x509Cert2, err := x509.ParseCertificate(cert2.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse second certificate: %v", err)
	}

	// They should be identical (same serial number)
	if x509Cert1.SerialNumber.Cmp(x509Cert2.SerialNumber) != 0 {
		t.Errorf("expected certificates to be identical (reused), but serial numbers differ: %v vs %v", x509Cert1.SerialNumber, x509Cert2.SerialNumber)
	}

	if fingerprint1 != fingerprint2 {
		t.Errorf("expected fingerprints to match, got %s and %s", fingerprint1, fingerprint2)
	}
}
