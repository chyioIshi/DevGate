package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadServerTLSConfigReturnsNilWhenTLSIsDisabled(t *testing.T) {
	config, err := loadServerTLSConfig("", "")
	if err != nil {
		t.Fatalf("loadServerTLSConfig() error = %v", err)
	}
	if config != nil {
		t.Errorf("loadServerTLSConfig() config = %#v, want nil", config)
	}
}

func TestLoadServerTLSConfigRejectsIncompletePair(t *testing.T) {
	for _, test := range []struct {
		name     string
		certFile string
		keyFile  string
	}{
		{
			name:     "certificate without key",
			certFile: "tls.crt",
		},
		{
			name:    "key without certificate",
			keyFile: "tls.key",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, err := loadServerTLSConfig(test.certFile, test.keyFile)
			if err == nil {
				t.Fatal("loadServerTLSConfig() error = nil, want validation error")
			}
			if config != nil {
				t.Errorf("loadServerTLSConfig() config = %#v, want nil", config)
			}
			if !strings.Contains(
				err.Error(),
				"TLS certificate and key files must be configured together",
			) {
				t.Errorf("loadServerTLSConfig() error = %q, want pair validation context", err)
			}
		})
	}
}

func TestLoadServerTLSConfigWrapsFileError(t *testing.T) {
	config, err := loadServerTLSConfig("missing.crt", "missing.key")
	if err == nil {
		t.Fatal("loadServerTLSConfig() error = nil, want file error")
	}
	if config != nil {
		t.Errorf("loadServerTLSConfig() config = %#v, want nil", config)
	}
	if !strings.Contains(err.Error(), "load TLS certificate and key") {
		t.Errorf("loadServerTLSConfig() error = %q, want load context", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("loadServerTLSConfig() error = %v, want fs.ErrNotExist", err)
	}
}

func TestLoadServerTLSConfigRejectsMismatchedPrivateKey(t *testing.T) {
	certFile, _ := writeTestCertificatePair(t)
	_, otherKeyFile := writeTestCertificatePair(t)

	config, err := loadServerTLSConfig(certFile, otherKeyFile)
	if err == nil {
		t.Fatal("loadServerTLSConfig() error = nil, want mismatched key error")
	}
	if config != nil {
		t.Errorf("loadServerTLSConfig() config = %#v, want nil", config)
	}
	if !strings.Contains(err.Error(), "load TLS certificate and key") {
		t.Errorf("loadServerTLSConfig() error = %q, want load context", err)
	}
}

func TestLoadServerTLSConfigLoadsCertificateAndSecureMinimumVersion(t *testing.T) {
	certFile, keyFile := writeTestCertificatePair(t)

	config, err := loadServerTLSConfig(certFile, keyFile)
	if err != nil {
		t.Fatalf("loadServerTLSConfig() error = %v", err)
	}
	if config == nil {
		t.Fatal("loadServerTLSConfig() config = nil, want TLS config")
	}
	if got := config.MinVersion; got != tls.VersionTLS12 {
		t.Errorf("TLS minimum version = %#x, want TLS 1.2", got)
	}
	if got := len(config.Certificates); got != 1 {
		t.Errorf("TLS certificate count = %d, want 1", got)
	}
}

func writeTestCertificatePair(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test private key: %v", err)
	}
	certificateTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		DNSNames:              []string{"localhost"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	certificateDER, err := x509.CreateCertificate(
		rand.Reader,
		certificateTemplate,
		certificateTemplate,
		&privateKey.PublicKey,
		privateKey,
	)
	if err != nil {
		t.Fatalf("create test certificate: %v", err)
	}
	privateKeyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal test private key: %v", err)
	}

	directory := t.TempDir()
	certFile := filepath.Join(directory, "tls.crt")
	keyFile := filepath.Join(directory, "tls.key")
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificateDER,
	})
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privateKeyDER,
	})
	if err := os.WriteFile(certFile, certificatePEM, 0o600); err != nil {
		t.Fatalf("write test certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, privateKeyPEM, 0o600); err != nil {
		t.Fatalf("write test private key: %v", err)
	}

	return certFile, keyFile
}
