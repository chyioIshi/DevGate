package redirect

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRedirectsRequest(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			handler, err := New(statusCode, "/api/v2", func(header http.Header) {
				header.Set("Cache-Control", "no-store")
				header.Set("Location", "/ignored")
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, "http://gateway.example/legacy", nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != statusCode {
				t.Errorf("status code = %d, want %d", response.Code, statusCode)
			}
			if got := response.Header().Get("Location"); got != "/api/v2" {
				t.Errorf("Location header = %q, want %q", got, "/api/v2")
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control header = %q, want %q", got, "no-store")
			}
		})
	}
}

func TestNewRejectsInvalidStatusCode(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusOK,
		http.StatusMultipleChoices,
		http.StatusNotModified,
		http.StatusBadRequest,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			got, err := New(statusCode, "/api/v2", nil)
			if err == nil {
				t.Fatal("New() error = nil, want invalid status error")
			}
			if got != nil {
				t.Errorf("New() handler = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), "301, 302, 303, 307, or 308") {
				t.Errorf("New() error = %q, want allowed statuses context", err)
			}
		})
	}
}

func TestHandlerDoesNotWriteBodyForHEADRequest(t *testing.T) {
	handler, err := New(http.StatusPermanentRedirect, "/api/v2", nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodHead, "http://gateway.example/legacy", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusPermanentRedirect {
		t.Errorf(
			"status code = %d, want %d",
			response.Code,
			http.StatusPermanentRedirect,
		)
	}
	if got := response.Header().Get("Location"); got != "/api/v2" {
		t.Errorf("Location header = %q, want %q", got, "/api/v2")
	}
	if response.Body.Len() != 0 {
		t.Errorf("response body = %q, want empty", response.Body.String())
	}
}
