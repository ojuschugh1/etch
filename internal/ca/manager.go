package ca

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertFile = "etch-ca.crt"
	caKeyFile  = "etch-ca.key"
	keyBits    = 2048
)

// CAManager generates and manages a local CA certificate used for TLS interception.
type CAManager struct {
	CADir  string
	CACert *x509.Certificate
	CAKey  crypto.PrivateKey
}

// NewCAManager creates a CAManager for the given directory and ensures the CA
// certificate and key are available (generating them if necessary).
func NewCAManager(caDir string) (*CAManager, error) {
	if caDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("ca: determine home directory: %w", err)
		}
		caDir = filepath.Join(home, ".etch")
	}

	m := &CAManager{CADir: caDir}
	if err := m.GetOrCreateCA(); err != nil {
		return nil, err
	}
	return m, nil
}

// GetOrCreateCA loads an existing CA cert and key from disk, or generates
// new ones if they do not exist.
func (m *CAManager) GetOrCreateCA() error {
	certPath := filepath.Join(m.CADir, caCertFile)
	keyPath := filepath.Join(m.CADir, caKeyFile)

	// Try loading existing files first.
	if certPEM, err := os.ReadFile(certPath); err == nil {
		if keyPEM, err := os.ReadFile(keyPath); err == nil {
			return m.loadFromPEM(certPEM, keyPEM)
		}
	}

	// Generate new CA.
	return m.generate(certPath, keyPath)
}

// CertPath returns the filesystem path to the CA certificate file.
func (m *CAManager) CertPath() string {
	return filepath.Join(m.CADir, caCertFile)
}

// GenerateServerCert creates a TLS leaf certificate for the given hostname,
// signed by the CA. The returned *tls.Certificate is ready for use in a
// tls.Config.
func (m *CAManager) GenerateServerCert(host string) (*tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("ca: generate server key: %w", err)
	}

	serial, err := randSerial()
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, m.CACert, &key.PublicKey, m.CAKey)
	if err != nil {
		return nil, fmt.Errorf("ca: sign server cert: %w", err)
	}

	tlsCert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	return tlsCert, nil
}

// generate creates a new self-signed CA certificate and RSA private key,
// writes them to disk as PEM files, and populates the manager fields.
func (m *CAManager) generate(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return fmt.Errorf("ca: generate key: %w", err)
	}

	serial, err := randSerial()
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Etch Local CA"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("ca: create certificate: %w", err)
	}

	// Ensure directory exists.
	if err := os.MkdirAll(m.CADir, 0o700); err != nil {
		return fmt.Errorf("ca: create directory %s: %w", m.CADir, err)
	}

	// Write cert PEM.
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return fmt.Errorf("ca: write cert: %w", err)
	}

	// Write key PEM.
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return fmt.Errorf("ca: write key: %w", err)
	}

	// Parse the DER cert back so we have a usable *x509.Certificate.
	parsed, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("ca: parse generated cert: %w", err)
	}
	m.CACert = parsed
	m.CAKey = key
	return nil
}

// loadFromPEM parses PEM-encoded certificate and key bytes and populates
// the manager fields.
func (m *CAManager) loadFromPEM(certPEM, keyPEM []byte) error {
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("ca: failed to decode certificate PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("ca: parse certificate: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("ca: failed to decode key PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("ca: parse private key: %w", err)
	}

	m.CACert = cert
	m.CAKey = key
	return nil
}

// randSerial generates a random serial number for an X.509 certificate.
func randSerial() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("ca: generate serial: %w", err)
	}
	return serial, nil
}
