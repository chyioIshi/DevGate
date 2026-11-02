package main

import (
	"crypto/tls"
	"errors"
	"fmt"
)

func loadServerTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	configuredCert := certFile != ""
	configuredKey := keyFile != ""
	if configuredCert != configuredKey {
		return nil, errors.New("TLS certificate and key files must be configured together")
	}
	if !configuredCert {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load TLS certificate and key: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, nil
}
