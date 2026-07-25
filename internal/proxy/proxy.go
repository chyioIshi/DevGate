package proxy

import (
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
)

func New(targetURL *url.URL, logger *slog.Logger) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.SetXForwarded()
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			logger.ErrorContext(
				req.Context(),
				"proxy request failed",
				"method", req.Method,
				"path", req.URL.Path,
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
