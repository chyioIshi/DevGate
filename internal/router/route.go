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
	PathExact           string
	HeaderMatches       []HeaderMatch
	Methods             []string
	Hosts               []string
	Priority            int
	UpstreamURL         *url.URL
	DirectResponse      *DirectResponse
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

// DirectResponse describes a static HTTP response returned for a matched route
// without forwarding the request to an upstream.
type DirectResponse struct {
	StatusCode int
	Body       string
}

// RateLimitPolicy defines the local token-bucket settings for a route.
type RateLimitPolicy struct {
	RequestsPerSecond float64
	Burst             int
}

// HeaderMatch defines an exact request header condition for a route.
type HeaderMatch struct {
	Name  string
	Exact string
}

func (r Route) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return errors.New("route name must not be empty")
	}

	if err := r.validateAction(); err != nil {
		return err
	}

	hasPathPrefix := r.PathPrefix != ""
	hasPathExact := r.PathExact != ""

	if hasPathExact && hasPathPrefix {
		return errors.New("path prefix and exact path are mutually exclusive")
	}
	if !hasPathExact && !hasPathPrefix {
		return errors.New("either path prefix or exact path must be configured")
	}
	if hasPathPrefix && !strings.HasPrefix(r.PathPrefix, "/") {
		return fmt.Errorf("path prefix %q must start with '/'", r.PathPrefix)
	}
	if hasPathPrefix && strings.HasSuffix(r.PathPrefix, "/") && r.PathPrefix != "/" {
		return fmt.Errorf(
			"path prefix %q must not end with '/' unless it is '/'",
			r.PathPrefix,
		)
	}
	if hasPathExact && !strings.HasPrefix(r.PathExact, "/") {
		return fmt.Errorf("exact path %q must start with '/'", r.PathExact)
	}
	if r.StripPathPrefix && !hasPathPrefix {
		return errors.New("strip path prefix requires a path prefix matcher")
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

	seenHosts := make(map[string]struct{}, len(r.Hosts))
	for _, host := range r.Hosts {
		if err := validateHostname(host); err != nil {
			return fmt.Errorf("invalid host %q: %w", host, err)
		}
		if _, exists := seenHosts[host]; exists {
			return fmt.Errorf("duplicate host: %q", host)
		}
		seenHosts[host] = struct{}{}
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

	seenHeaderMatches := make(map[string]struct{}, len(r.HeaderMatches))
	for _, headerMatch := range r.HeaderMatches {
		if err := headerMatch.validate(); err != nil {
			return fmt.Errorf("header match policy: %w", err)
		}
		normalized := strings.ToLower(headerMatch.Name)
		if _, exists := seenHeaderMatches[normalized]; exists {
			return fmt.Errorf(
				"header match policy: duplicate header match for header: %q",
				normalized,
			)
		}
		seenHeaderMatches[normalized] = struct{}{}
	}
	return nil
}

func (r Route) validateAction() error {
	if r.DirectResponse == nil && r.UpstreamURL == nil {
		return errors.New("either upstream URL or direct response must be configured")
	}
	if r.DirectResponse != nil && r.UpstreamURL != nil {
		return errors.New("upstream URL and direct response are mutually exclusive")
	}
	if r.UpstreamURL != nil {
		if r.Protocol != ProtocolHTTP && r.Protocol != ProtocolGRPC {
			return fmt.Errorf("unsupported protocol %q", r.Protocol)
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
	}
	if r.DirectResponse != nil {
		if r.Protocol != "" {
			return errors.New("protocol is not supported for direct response")
		}
		if r.RequestHeaders != nil {
			return errors.New("request headers are not supported for direct response")
		}
		if r.StripPathPrefix {
			return errors.New("strip path prefix is not supported for direct response")
		}
		if r.RequestTimeout != 0 {
			return errors.New("request timeout is not supported for direct response")
		}
		if r.MaxRequestBodyBytes != 0 {
			return errors.New("max request body bytes is not supported for direct response")
		}
		if err := r.DirectResponse.validate(); err != nil {
			return fmt.Errorf("direct response validation: %w", err)
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
	return strings.HasPrefix(lowerName, "x-forwarded-")
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

func validateHostname(host string) error {
	if len(host) > 253 {
		return errors.New("host cannot exceed 253 characters")
	}
	if strings.TrimSpace(host) == "" {
		return errors.New("host cannot be empty")
	}
	if strings.ToLower(host) != host {
		return errors.New("host must be lowercase")
	}
	if strings.HasSuffix(host, ".") {
		return errors.New("host cannot have a trailing dot")
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 {
			return errors.New("host label cannot be empty")
		}
		if len(label) > 63 {
			return errors.New("host label cannot exceed 63 characters")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return errors.New("host label cannot start or end with a hyphen")
		}
		for _, char := range label {
			if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
				return errors.New("host label contains invalid character")
			}
		}
	}
	return nil
}

func (d DirectResponse) validate() error {
	if d.StatusCode < 200 || d.StatusCode > 599 {
		return errors.New("direct response status code must be between 200 and 599")
	}
	if d.Body != "" {
		switch d.StatusCode {
		case http.StatusNoContent,
			http.StatusResetContent,
			http.StatusNotModified:
			return fmt.Errorf("response status %d must not include a body", d.StatusCode)
		}
	}
	return nil
}

func (h HeaderMatch) validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("header name cannot be empty")
	}
	if strings.TrimSpace(h.Exact) == "" {
		return errors.New("header value cannot be empty")
	}
	if !httpguts.ValidHeaderFieldName(h.Name) {
		return fmt.Errorf("invalid header name: %q", h.Name)
	}
	if !httpguts.ValidHeaderFieldValue(h.Exact) {
		return fmt.Errorf("invalid value for header %q: %q", h.Name, h.Exact)
	}
	if isReservedRequestHeader(h.Name) {
		return fmt.Errorf("header %q is reserved and cannot be matched", h.Name)
	}
	return nil
}
