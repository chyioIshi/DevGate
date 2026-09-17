package router

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/http/httpguts"
)

type Protocol string

const (
	ProtocolHTTP Protocol = "http"
	ProtocolGRPC Protocol = "grpc"
)

type Route struct {
	Name                string
	Protocol            Protocol
	PathPrefix          string
	Methods             []string
	UpstreamURL         *url.URL
	RequestHeaders      *HeaderTransformPolicy
	ResponseHeaders     *HeaderTransformPolicy
	RateLimit           *RateLimitPolicy
	StripPathPrefix     bool
	RequestTimeout      time.Duration
	MaxRequestBodyBytes int64
}

// HeaderTransformPolicy describes static header values to set and header names
// to remove when transforming a request or response for a route.
type HeaderTransformPolicy struct {
	Set    map[string]string
	Remove []string
}

// RateLimitPolicy defines the local token-bucket settings for a route.
type RateLimitPolicy struct {
	RequestsPerSecond float64
	Burst             int
}

func (r Route) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("route name must not be empty")
	}
	if r.Protocol != ProtocolHTTP && r.Protocol != ProtocolGRPC {
		return fmt.Errorf("unsupported protocol %q", r.Protocol)
	}
	if !strings.HasPrefix(r.PathPrefix, "/") {
		return fmt.Errorf("path prefix %q must start with '/'", r.PathPrefix)
	}
	if strings.HasSuffix(r.PathPrefix, "/") && r.PathPrefix != "/" {
		return fmt.Errorf(
			"path prefix %q must not end with '/' unless it is '/'",
			r.PathPrefix,
		)
	}
	seenMethods := make(map[string]struct{}, len(r.Methods))
	for _, method := range r.Methods {
		if strings.TrimSpace(method) == "" {
			return errors.New("HTTP method must not be empty")
		}
		if strings.ToUpper(method) != method {
			return fmt.Errorf("HTTP method must be uppercase: %q", method)
		}
		if !httpguts.ValidHeaderFieldName(method) {
			return fmt.Errorf("invalid HTTP method: %q", method)
		}
		if _, exists := seenMethods[method]; exists {
			return fmt.Errorf("duplicate HTTP method: %q", method)
		}
		seenMethods[method] = struct{}{}
	}

	if r.UpstreamURL == nil {
		return errors.New("upstream URL must not be nil")
	}

	if r.UpstreamURL.Scheme != "http" && r.UpstreamURL.Scheme != "https" {
		return fmt.Errorf(
			"upstream URL scheme %q must be either 'http' or 'https'",
			r.UpstreamURL.Scheme,
		)
	}
	if strings.TrimSpace(r.UpstreamURL.Host) == "" {
		return errors.New("upstream URL host must not be empty")
	}
	if r.RequestTimeout < 0 {
		return errors.New("request timeout must not be negative")
	}
	if r.MaxRequestBodyBytes < 0 {
		return errors.New("max request body bytes must not be negative")
	}
	if r.RequestHeaders != nil {
		if err := r.RequestHeaders.validate(isReservedRequestHeader); err != nil {
			return fmt.Errorf("request headers policy: %w", err)
		}
	}
	if r.ResponseHeaders != nil {
		if err := r.ResponseHeaders.validate(isReservedResponseHeader); err != nil {
			return fmt.Errorf("response headers policy: %w", err)
		}
	}
	if r.RateLimit != nil {
		if err := r.RateLimit.validate(); err != nil {
			return fmt.Errorf("rate limit policy: %w", err)
		}
	}
	return nil
}

func (p HeaderTransformPolicy) validate(isReserved func(string) bool) error {
	setNames := make(map[string]struct{}, len(p.Set))
	removeNames := make(map[string]struct{}, len(p.Remove))
	for header, value := range p.Set {
		if !httpguts.ValidHeaderFieldName(header) {
			return fmt.Errorf("invalid header name to set: %q", header)
		}
		if isReserved(header) {
			return fmt.Errorf("header %q is reserved and cannot be modified", header)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("invalid value for header %q", header)
		}
		normalized := strings.ToLower(header)
		if _, exists := setNames[normalized]; exists {
			return fmt.Errorf(
				"duplicate header name to set: %q",
				normalized,
			)
		}
		setNames[normalized] = struct{}{}
	}
	for _, header := range p.Remove {
		if !httpguts.ValidHeaderFieldName(header) {
			return fmt.Errorf("invalid header name to remove: %q", header)
		}
		if isReserved(header) {
			return fmt.Errorf("header %q is reserved and cannot be modified", header)
		}
		normalized := strings.ToLower(header)
		if _, exists := removeNames[normalized]; exists {
			return fmt.Errorf(
				"duplicate header name to remove: %q",
				normalized,
			)
		}
		if _, exists := setNames[normalized]; exists {
			return fmt.Errorf(
				"header %q cannot be both set and removed",
				normalized,
			)
		}
		removeNames[normalized] = struct{}{}
	}
	return nil
}

func isReservedRequestHeader(name string) bool {
	lowerName := strings.ToLower(name)

	switch lowerName {
	case "host",
		"connection",
		"content-length",
		"forwarded",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"proxy-connection",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"x-real-ip",
		"x-request-id":
		return true
	}
	if strings.HasPrefix(lowerName, "x-forwarded-") {
		return true
	}
	return false
}

func isReservedResponseHeader(name string) bool {
	lowerName := strings.ToLower(name)

	switch lowerName {
	case "connection",
		"content-length",
		"content-encoding",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"proxy-connection",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"x-request-id":
		return true
	}
	return false
}

// Apply mutates header in place by removing configured fields and replacing
// configured fields with their static values.
func (p HeaderTransformPolicy) Apply(header http.Header) {
	for _, name := range p.Remove {
		header.Del(name)
	}
	for name, value := range p.Set {
		header.Set(name, value)
	}
}

func (p RateLimitPolicy) validate() error {
	if math.IsInf(p.RequestsPerSecond, 0) || math.IsNaN(p.RequestsPerSecond) {
		return errors.New("requests per second must be finite")
	}
	if p.RequestsPerSecond <= 0 {
		return errors.New("requests per second must be positive")
	}
	if p.Burst <= 0 {
		return errors.New("burst must be positive")
	}
	return nil
}
