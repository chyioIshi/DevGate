package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chyioishi/devgate/internal/config"
)

func TestGlobalErrorResponderFromConfig(t *testing.T) {
	responses := map[int]config.ErrorResponseConfig{
		http.StatusNotFound: {
			Body:    `{"error":"route not found"}`,
			Headers: map[string]string{"Content-Type": "application/json"},
		},
		http.StatusMethodNotAllowed: {
			Body: `{"error":"method not allowed"}`,
		},
		http.StatusInternalServerError: {
			Body: `{"error":"internal server error"}`,
		},
	}

	responder, err := globalErrorResponderFromConfig(responses)
	if err != nil {
		t.Fatalf("globalErrorResponderFromConfig() error = %v", err)
	}

	for statusCode, response := range responses {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "http://gateway.local/missing", nil)

			responder.Write(recorder, request, statusCode)

			if recorder.Code != statusCode {
				t.Errorf("status code = %d, want %d", recorder.Code, statusCode)
			}
			if got := recorder.Body.String(); got != response.Body {
				t.Errorf("body = %q, want %q", got, response.Body)
			}
			for name, want := range response.Headers {
				if got := recorder.Header().Get(name); got != want {
					t.Errorf("header %q = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestGlobalErrorResponderFromConfigUsesFallbackForUnconfiguredStatus(t *testing.T) {
	responder, err := globalErrorResponderFromConfig(nil)
	if err != nil {
		t.Fatalf("globalErrorResponderFromConfig() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/missing", nil)
	responder.Write(recorder, request, http.StatusNotFound)

	if recorder.Code != http.StatusNotFound {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if got, want := recorder.Body.String(), "Not Found\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestGlobalErrorResponderFromConfigRejectsUnsupportedStatus(t *testing.T) {
	unsupportedStatuses := []int{
		http.StatusBadRequest,
		http.StatusRequestEntityTooLarge,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	}

	for _, statusCode := range unsupportedStatuses {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			_, err := globalErrorResponderFromConfig(
				map[int]config.ErrorResponseConfig{statusCode: {}},
			)
			if err == nil {
				t.Fatal("globalErrorResponderFromConfig() error = nil, want non-nil")
			}
			const want = "global error response status code must be one of 404, 405, or 500"
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err, want)
			}
		})
	}
}

func TestGlobalErrorResponderFromConfigWrapsResponderError(t *testing.T) {
	_, err := globalErrorResponderFromConfig(map[int]config.ErrorResponseConfig{
		http.StatusNotFound: {
			Headers: map[string]string{"Content-Length": "42"},
		},
	})
	if err == nil {
		t.Fatal("globalErrorResponderFromConfig() error = nil, want non-nil")
	}
	if got := err.Error(); !strings.Contains(got, "create global error responder:") {
		t.Errorf("error = %q, want constructor context", got)
	}
}

func TestGlobalErrorResponderFromConfigCopiesResponses(t *testing.T) {
	responses := map[int]config.ErrorResponseConfig{
		http.StatusNotFound: {
			Body:    "original body",
			Headers: map[string]string{"Content-Type": "text/plain"},
		},
	}

	responder, err := globalErrorResponderFromConfig(responses)
	if err != nil {
		t.Fatalf("globalErrorResponderFromConfig() error = %v", err)
	}
	response := responses[http.StatusNotFound]
	response.Body = "mutated body"
	response.Headers["Content-Type"] = "application/json"
	responses[http.StatusNotFound] = response

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://gateway.local/missing", nil)
	responder.Write(recorder, request, http.StatusNotFound)

	if got, want := recorder.Body.String(), "original body"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if got, want := recorder.Header().Get("Content-Type"), "text/plain"; got != want {
		t.Errorf("Content-Type = %q, want %q", got, want)
	}
}
