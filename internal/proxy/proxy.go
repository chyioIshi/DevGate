package proxy

import (
	"net/http/httputil"
	"net/url"
)

func New(targetURL *url.URL) *httputil.ReverseProxy {
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(targetURL)
			pr.SetXForwarded()
		},
	}
	return proxy
}
