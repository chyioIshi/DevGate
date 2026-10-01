package proxy

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestReleasingTransportReleasesOnRoundTripError(t *testing.T) {
	wantErr := errors.New("round trip failed")
	releaseCount := 0
	transport := &releasingTransport{
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, wantErr
		}),
	}

	//nolint:bodyclose // The error path under test must return a nil response.
	response, err := transport.RoundTrip(requestWithRelease(func() {
		releaseCount++
	}))
	if !errors.Is(err, wantErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, wantErr)
	}
	if response != nil {
		t.Errorf("RoundTrip() response = %#v, want nil", response)
	}
	if releaseCount != 1 {
		t.Errorf("release count = %d, want 1", releaseCount)
	}
}

func TestReleasingTransportReleasesWhenResponseBodyCloses(t *testing.T) {
	releaseCount := 0
	transport := &releasingTransport{
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString("response")),
			}, nil
		}),
	}

	response, err := transport.RoundTrip(requestWithRelease(func() {
		releaseCount++
	}))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if releaseCount != 0 {
		t.Fatalf("release count before body consumption = %d, want 0", releaseCount)
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if releaseCount != 0 {
		t.Fatalf("release count before body close = %d, want 0", releaseCount)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body a second time: %v", err)
	}
	if releaseCount != 1 {
		t.Errorf("release count after repeated body close = %d, want 1", releaseCount)
	}
}

func TestReleasingTransportPreservesReadWriteCloser(t *testing.T) {
	releaseCount := 0
	body := &testReadWriteCloser{}
	transport := &releasingTransport{
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusSwitchingProtocols,
				Body:       body,
			}, nil
		}),
	}

	response, err := transport.RoundTrip(requestWithRelease(func() {
		releaseCount++
	}))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	readWriteBody, ok := response.Body.(io.ReadWriteCloser)
	if !ok {
		t.Fatalf("response body type = %T, want io.ReadWriteCloser", response.Body)
	}
	if _, err := readWriteBody.Write([]byte("websocket frame")); err != nil {
		t.Fatalf("write response body: %v", err)
	}
	if got := body.String(); got != "websocket frame" {
		t.Errorf("written body = %q, want %q", got, "websocket frame")
	}
	if err := readWriteBody.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	if releaseCount != 1 {
		t.Errorf("release count = %d, want 1", releaseCount)
	}
}

func TestReleasingTransportReleasesResponseWithoutBody(t *testing.T) {
	releaseCount := 0
	transport := &releasingTransport{
		next: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent}, nil
		}),
	}

	//nolint:bodyclose // The response-without-body path is the behavior under test.
	_, err := transport.RoundTrip(requestWithRelease(func() {
		releaseCount++
	}))
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if releaseCount != 1 {
		t.Errorf("release count = %d, want 1", releaseCount)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func requestWithRelease(release func()) *http.Request {
	request, _ := http.NewRequest(http.MethodGet, "http://upstream.example", nil)
	ctx := context.WithValue(request.Context(), releaseContextKey{}, release)
	return request.WithContext(ctx)
}

type testReadWriteCloser struct {
	bytes.Buffer
}

func (*testReadWriteCloser) Close() error {
	return nil
}
