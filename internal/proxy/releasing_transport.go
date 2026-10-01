package proxy

import (
	"io"
	"net/http"
	"sync"
)

type releasingTransport struct {
	next http.RoundTripper
}

func (t *releasingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	release := req.Context().Value(releaseContextKey{}).(func())
	resp, err := t.next.RoundTrip(req)
	if err != nil {
		release()
		return nil, err
	}

	if resp.Body != nil {
		readWriteCloser, ok := resp.Body.(io.ReadWriteCloser)
		if ok {
			resp.Body = &releasingReadWriteBody{
				ReadWriteCloser: readWriteCloser,
				release:         release,
			}
		} else {
			resp.Body = &releasingBody{
				ReadCloser: resp.Body,
				release:    release,
			}
		}
	} else {
		release()
	}
	return resp, nil
}

type releasingBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (r *releasingBody) Close() error {
	defer r.once.Do(r.release)
	return r.ReadCloser.Close()
}

type releasingReadWriteBody struct {
	io.ReadWriteCloser
	release func()
	once    sync.Once
}

func (r *releasingReadWriteBody) Close() error {
	defer r.once.Do(r.release)
	return r.ReadWriteCloser.Close()
}
