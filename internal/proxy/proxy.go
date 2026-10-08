package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"slices"

	"github.com/chyioishi/devgate/internal/requestid"
)

// ErrNoAvailableUpstream indicates that no upstream can currently accept the
// request.
var ErrNoAvailableUpstream = errors.New("no available upstream")

// RequestHeaderTransform modifies the headers of a request before it is sent
// to the upstream server.
type RequestHeaderTransform func(http.Header)

// ResponseHeaderTransform modifies the headers of a response received from
// the upstream server before it is sent to the client.
type ResponseHeaderTransform func(http.Header)

// ErrorResponder writes a gateway-generated HTTP error for a failed proxy
// request.
type ErrorResponder func(http.ResponseWriter, *http.Request, int)

type releaseContextKey struct{}

type acquiredTargetContextKey struct{}

// Handler proxies requests to targets selected by a TargetPicker.
type Handler struct {
	targetPicker   TargetPicker
	reverseProxy   *httputil.ReverseProxy
	errorResponder ErrorResponder
	logger         *slog.Logger
}

type acquiredTarget struct {
	target  url.URL
	release func()
}

// TargetPicker selects an upstream target for an incoming proxy request.
// On success, Acquire returns a non-nil idempotent release function that the
// caller must invoke after request processing finishes.
type TargetPicker interface {
	Acquire() (url.URL, func(), error)
}

// New creates a proxy handler that selects an upstream target for each
// incoming request using targetPicker.
func New(
	targetPicker TargetPicker,
	transport http.RoundTripper,
	requestHeaderTransform RequestHeaderTransform,
	responseHeaderTransform ResponseHeaderTransform,
	trustedCIDRs []netip.Prefix,
	errorResponder ErrorResponder,
	logger *slog.Logger,
) *Handler {
	trustedCIDRs = slices.Clone(trustedCIDRs)
	proxy := &httputil.ReverseProxy{
		Transport: &releasingTransport{next: transport},
		Rewrite: func(pr *httputil.ProxyRequest) {
			acquiredTarget := pr.In.Context().Value(acquiredTargetContextKey{}).(acquiredTarget)

			ctx := context.WithValue(
				pr.Out.Context(),
				releaseContextKey{},
				acquiredTarget.release,
			)
			pr.Out = pr.Out.WithContext(ctx)
			pr.SetURL(&acquiredTarget.target)
			// Share the trailer map so values populated at inbound body EOF are
			// available to the outbound transport.
			pr.Out.Trailer = pr.In.Trailer
			if requestHeaderTransform != nil {
				requestHeaderTransform(pr.Out.Header)
			}
			pr.SetXForwarded()
			setXForwardedFor(pr, trustedCIDRs)
		},
		ModifyResponse: func(response *http.Response) error {
			if responseHeaderTransform != nil {
				responseHeaderTransform(response.Header)
			}
			response.Header.Del(requestid.HeaderName)
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			handleProxyError(rw, req, err, errorResponder, logger)
		},
	}
	return &Handler{
		targetPicker:   targetPicker,
		reverseProxy:   proxy,
		errorResponder: errorResponder,
		logger:         logger,
	}
}

func (h *Handler) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	target, release, err := h.targetPicker.Acquire()
	if err != nil {
		selectionErr := fmt.Errorf(
			"%w: target selection failed: %w",
			ErrNoAvailableUpstream,
			err,
		)
		handleProxyError(rw, req, selectionErr, h.errorResponder, h.logger)
		return
	}
	defer release()
	ctx := context.WithValue(
		req.Context(),
		acquiredTargetContextKey{},
		acquiredTarget{
			target:  target,
			release: release,
		},
	)
	req = req.WithContext(ctx)
	h.reverseProxy.ServeHTTP(rw, req)
}

type timeoutReporter interface {
	Timeout() bool
}

func handleProxyError(
	rw http.ResponseWriter,
	req *http.Request,
	err error,
	errorResponder ErrorResponder,
	logger *slog.Logger,
) {
	requestID, _ := requestid.FromContext(req.Context())
	logger.ErrorContext(
		req.Context(),
		"proxy request failed",
		"method", req.Method,
		"path", req.URL.Path,
		"request_id", requestID,
		"error", err,
	)
	statusCode := statusCodeForProxyError(err)
	if errorResponder != nil {
		errorResponder(rw, req, statusCode)
	} else {
		http.Error(
			rw,
			http.StatusText(statusCode),
			statusCode,
		)
	}
}

func statusCodeForProxyError(err error) int {
	var reporter timeoutReporter
	var maxBytesError *http.MaxBytesError

	switch {
	case errors.Is(err, ErrCircuitOpen),
		errors.Is(err, ErrNoAvailableUpstream):
		return http.StatusServiceUnavailable
	case errors.As(err, &maxBytesError):
		return http.StatusRequestEntityTooLarge
	case errors.As(err, &reporter) && reporter.Timeout():
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}
