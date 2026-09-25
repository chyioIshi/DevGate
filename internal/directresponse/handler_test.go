package directresponse

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRejectsInvalidStatusCode(t *testing.T) {
	for _, statusCode := range []int{199, 600} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			got, err := New(statusCode, "", nil)
			if err == nil {
				t.Fatalf("New(%d) error = nil, want error", statusCode)
			}
			if got != nil {
				t.Errorf("New(%d) handler = %#v, want nil", statusCode, got)
			}
			if !strings.Contains(err.Error(), "invalid status code") {
				t.Errorf("New(%d) error = %q, want invalid status context", statusCode, err)
			}
		})
	}
}

func TestNewRejectsBodyForStatusWithoutContent(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusNoContent,
		http.StatusResetContent,
		http.StatusNotModified,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			got, err := New(statusCode, "unexpected body", nil)
			if err == nil {
				t.Fatalf("New(%d) error = nil, want error", statusCode)
			}
			if got != nil {
				t.Errorf("New(%d) handler = %#v, want nil", statusCode, got)
			}
			wantMessage := fmt.Sprintf("response status %d must not include a body", statusCode)
			if !strings.Contains(err.Error(), wantMessage) {
				t.Errorf("New(%d) error = %q, want context %q", statusCode, err, wantMessage)
			}
		})
	}
}

func TestNewAllowsBodyForStatusUseProxy(t *testing.T) {
	got, err := New(http.StatusUseProxy, "body", nil)
	if err != nil {
		t.Fatalf("New(%d) error = %v", http.StatusUseProxy, err)
	}
	if got == nil {
		t.Fatalf("New(%d) handler = nil, want non-nil handler", http.StatusUseProxy)
	}
}

func TestHandlerWritesConfiguredResponse(t *testing.T) {
	handler, err := New(http.StatusServiceUnavailable, "maintenance", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api", nil)
	handler.ServeHTTP(recorder, request)

	response := recorder.Result()
	defer closeResponseBody(t, response.Body)

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(
			"status code = %d, want %d",
			response.StatusCode,
			http.StatusServiceUnavailable,
		)
	}
	if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if got := string(body); got != "maintenance" {
		t.Errorf("body = %q, want %q", got, "maintenance")
	}
}

func TestHandlerAppliesHeaderTransformAfterDefaults(t *testing.T) {
	handler, err := New(http.StatusOK, "{}", func(header http.Header) {
		header.Set("Content-Type", "application/json")
		header.Set("X-Direct-Response", "true")
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api", nil))
	response := recorder.Result()
	defer closeResponseBody(t, response.Body)

	if got := response.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := response.Header.Get("X-Direct-Response"); got != "true" {
		t.Errorf("X-Direct-Response = %q, want %q", got, "true")
	}
}

func TestHandlerHEADReturnsHeadersWithoutBody(t *testing.T) {
	handler, err := New(http.StatusServiceUnavailable, "maintenance", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodHead, "/api", nil))
	response := recorder.Result()
	defer closeResponseBody(t, response.Body)

	if response.StatusCode != http.StatusServiceUnavailable {
		t.Errorf(
			"status code = %d, want %d",
			response.StatusCode,
			http.StatusServiceUnavailable,
		)
	}
	if got := response.Header.Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want empty body", body)
	}
}

func TestHandlerWithEmptyBodyDoesNotSetContentType(t *testing.T) {
	handler, err := New(http.StatusNoContent, "", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api", nil))
	response := recorder.Result()
	defer closeResponseBody(t, response.Body)

	if got := response.Header.Get("Content-Type"); got != "" {
		t.Errorf("Content-Type = %q, want empty header", got)
	}
}

func closeResponseBody(t *testing.T, body io.Closer) {
	t.Helper()
	if err := body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
}
