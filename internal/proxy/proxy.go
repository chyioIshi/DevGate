package proxy

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/chyioishi/devgate/internal/requestid"
)

// RequestHeaderTransform modifies the headers of a request before it is sent
// to the upstream server.
type RequestHeaderTransform func(http.Header)

func New(
	targetURL *url.URL,
	transport http.RoundTripper,
	requestHeaderTransform RequestHeaderTransform,
	logger *slog.Logger,
) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			if requestHeaderTransform != nil {
				requestHeaderTransform(pr.Out.Header)
			}
			pr.SetXForwarded()
		},
		ModifyResponse: func(response *http.Response) error {
			response.Header.Del(requestid.HeaderName)
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
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
			http.Error(
				rw,
				http.StatusText(statusCode),
				statusCode,
			)
		},
	}
	return proxy
}

type timeoutReporter interface {
	Timeout() bool
}

func statusCodeForProxyError(err error) int {
	var reporter timeoutReporter
	var maxBytesError *http.MaxBytesError

	if errors.Is(err, ErrCircuitOpen) {
		return http.StatusServiceUnavailable
	}

	if errors.As(err, &maxBytesError) {
		return http.StatusRequestEntityTooLarge
	}

	if errors.As(err, &reporter) && reporter.Timeout() {
		return http.StatusGatewayTimeout
	}
	return http.StatusBadGateway
}
