package proxy

import (
	"errors"
	"fmt"
	"net/http"
	"time"
)

// TransportConfig defines connection-pool limits and response timing for the
// shared upstream HTTP transport.
type TransportConfig struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	MaxConnsPerHost       int
	IdleConnTimeout       time.Duration
	ResponseHeaderTimeout time.Duration
}

// NewTransport clones the default HTTP transport and applies explicit upstream
// connection-pool limits and timeouts.
func NewTransport(config TransportConfig) (*http.Transport, error) {
	if config.MaxIdleConns <= 0 {
		return nil, errors.New("upstream max idle connections must be positive")
	}
	if config.MaxIdleConnsPerHost <= 0 {
		return nil, errors.New("upstream max idle connections per host must be positive")
	}
	if config.MaxConnsPerHost <= 0 {
		return nil, errors.New("upstream max connections per host must be positive")
	}
	if config.IdleConnTimeout <= 0 {
		return nil, errors.New("upstream idle connection timeout must be positive")
	}
	if config.ResponseHeaderTimeout <= 0 {
		return nil, errors.New("upstream response header timeout must be positive")
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unexpected type %T", http.DefaultTransport)
	}
	transportClone := transport.Clone()
	transportClone.MaxIdleConns = config.MaxIdleConns
	transportClone.MaxIdleConnsPerHost = config.MaxIdleConnsPerHost
	transportClone.MaxConnsPerHost = config.MaxConnsPerHost
	transportClone.IdleConnTimeout = config.IdleConnTimeout
	transportClone.ResponseHeaderTimeout = config.ResponseHeaderTimeout
	return transportClone, nil
}
