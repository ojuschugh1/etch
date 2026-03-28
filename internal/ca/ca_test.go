package ca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCAManager_CreatesCertAndKeyFiles(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("NewCAManager failed: %v", err)
	}

	certPath := filepath.Join(dir, caCertFile)
	keyPath := filepath.Join(dir, caKeyFile)

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Fatalf("CA cert file not created at %s", certPath)
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Fatalf("CA key file not created at %s", keyPath)
	}

	// Verify the cert file is valid PEM
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("reading cert file: %v", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatal("cert file does not contain a valid CERTIFICATE PEM block")
	}

	// Verify the key file is valid PEM
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("reading key file: %v", err)
	}
	block, _ = pem.Decode(keyPEM)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		t.Fatal("key file does not contain a valid RSA PRIVATE KEY PEM block")
	}

	// Verify manager fields are populated
	if mgr.CACert == nil {
		t.Fatal("CACert is nil after creation")
	}
	if mgr.CAKey == nil {
		t.Fatal("CAKey is nil after creation")
	}
	if !mgr.CACert.IsCA {
		t.Error("generated certificate is not a CA")
	}
}

// Calling NewCAManager twice on the same dir should reuse the existing cert.
func TestGetOrCreateCA_Idempotent(t *testing.T) {
	dir := t.TempDir()

	mgr1, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("first NewCAManager failed: %v", err)
	}

	// Read the cert file bytes after first creation
	certBytes1, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		t.Fatalf("reading cert after first call: %v", err)
	}
	keyBytes1, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		t.Fatalf("reading key after first call: %v", err)
	}

	// Create a second manager pointing at the same directory
	mgr2, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("second NewCAManager failed: %v", err)
	}

	// Files on disk should be unchanged
	certBytes2, err := os.ReadFile(filepath.Join(dir, caCertFile))
	if err != nil {
		t.Fatalf("reading cert after second call: %v", err)
	}
	keyBytes2, err := os.ReadFile(filepath.Join(dir, caKeyFile))
	if err != nil {
		t.Fatalf("reading key after second call: %v", err)
	}

	if string(certBytes1) != string(certBytes2) {
		t.Error("cert file changed between calls - GetOrCreateCA is not idempotent")
	}
	if string(keyBytes1) != string(keyBytes2) {
		t.Error("key file changed between calls - GetOrCreateCA is not idempotent")
	}

	// Both managers should have loaded the same CA cert serial number
	if mgr1.CACert.SerialNumber.Cmp(mgr2.CACert.SerialNumber) != 0 {
		t.Error("CA serial numbers differ - second call did not reuse existing CA")
	}
}

func TestGenerateServerCert_ValidTLSCert(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("NewCAManager failed: %v", err)
	}

	host := "api.example.com"
	tlsCert, err := mgr.GenerateServerCert(host)
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	if tlsCert == nil {
		t.Fatal("returned tls.Certificate is nil")
	}
	if len(tlsCert.Certificate) == 0 {
		t.Fatal("tls.Certificate has no certificate data")
	}
	if tlsCert.PrivateKey == nil {
		t.Fatal("tls.Certificate has no private key")
	}

	// Parse the leaf certificate and verify properties
	leaf, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		t.Fatalf("parsing leaf cert: %v", err)
	}

	if leaf.Subject.CommonName != host {
		t.Errorf("CommonName = %q, want %q", leaf.Subject.CommonName, host)
	}

	foundDNS := false
	for _, name := range leaf.DNSNames {
		if name == host {
			foundDNS = true
			break
		}
	}
	if !foundDNS {
		t.Errorf("DNSNames %v does not contain %q", leaf.DNSNames, host)
	}

	if leaf.IsCA {
		t.Error("server cert should not be a CA")
	}

	// Verify the cert is signed by the CA
	roots := x509.NewCertPool()
	roots.AddCert(mgr.CACert)
	opts := x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if _, err := leaf.Verify(opts); err != nil {
		t.Fatalf("server cert does not verify against CA: %v", err)
	}
}

func TestCertPath_ReturnsCorrectPath(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("NewCAManager failed: %v", err)
	}

	expected := filepath.Join(dir, caCertFile)
	got := mgr.CertPath()
	if got != expected {
		t.Errorf("CertPath() = %q, want %q", got, expected)
	}
}

// Quick sanity check that the cert works in a real tls.Config.
func TestGenerateServerCert_UsableInTLSConfig(t *testing.T) {
	dir := t.TempDir()

	mgr, err := NewCAManager(dir)
	if err != nil {
		t.Fatalf("NewCAManager failed: %v", err)
	}

	tlsCert, err := mgr.GenerateServerCert("localhost")
	if err != nil {
		t.Fatalf("GenerateServerCert failed: %v", err)
	}

	// Verify the cert can be assigned to a tls.Config without panic
	cfg := &tls.Config{
		Certificates: []tls.Certificate{*tlsCert},
	}
	if len(cfg.Certificates) != 1 {
		t.Error("expected exactly one certificate in TLS config")
	}
}
