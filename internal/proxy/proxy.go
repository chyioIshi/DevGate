package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/chyioishi/devgate/internal/requestid"
)

func New(targetURL *url.URL, logger *slog.Logger) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.SetXForwarded()
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
			http.Error(
				rw,
				http.StatusText(http.StatusBadGateway),
				http.StatusBadGateway,
			)
		},
	}
	return proxy
}
