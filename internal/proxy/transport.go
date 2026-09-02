package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// NewTransport creates an HTTP transport with a bounded wait for upstream response headers.
func NewTransport(responseHeaderTimeout time.Duration) (*http.Transport, error) {
	if responseHeaderTimeout <= 0 {
		return nil, errors.New("upstream response header timeout must be positive")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unexpected type %T", http.DefaultTransport)
	}
	transportClone := transport.Clone()
	transportClone.ResponseHeaderTimeout = responseHeaderTimeout
	return transportClone, nil
}
