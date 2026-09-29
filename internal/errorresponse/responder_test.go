package errorresponse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewAcceptsNilResponses(t *testing.T) {
	got, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if got == nil {
		t.Fatal("New() responder = nil, want non-nil responder")
	}
}

func TestNewRejectsInvalidStatusCode(t *testing.T) {
	for _, statusCode := range []int{http.StatusPermanentRedirect, 600} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			got, err := New(map[int]Response{statusCode: {}})
			if err == nil {
				t.Fatal("New() error = nil, want invalid status error")
			}
			if got != nil {
				t.Errorf("New() responder = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), "must be between 400 and 599") {
				t.Errorf("New() error = %q, want status range context", err)
			}
		})
	}
}

func TestNewValidatesHeaders(t *testing.T) {
	tests := []struct {
		name        string
		headers     map[string]string
		wantMessage string
	}{
		{
			name:    "valid headers",
			headers: map[string]string{"Content-Type": "application/json", "Cache-Control": "no-store"},
		},
		{
			name:        "invalid name",
			headers:     map[string]string{"Invalid Header": "value"},
			wantMessage: `invalid header name "Invalid Header"`,
		},
		{
			name:        "invalid value",
			headers:     map[string]string{"X-Reason": "failed\r\nX-Evil: true"},
			wantMessage: `invalid value "failed\r\nX-Evil: true" for header "X-Reason"`,
		},
		{
			name: "case insensitive duplicate",
			headers: map[string]string{
				"Content-Type": "application/json",
				"content-type": "text/plain",
			},
			wantMessage: "duplicate header name",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := New(map[int]Response{
				http.StatusBadGateway: {Headers: test.headers},
			})
			if test.wantMessage == "" {
				if err != nil {
					t.Fatalf("New() error = %v", err)
				}
				if got == nil {
					t.Fatal("New() responder = nil, want non-nil responder")
				}
				return
			}

			if err == nil {
				t.Fatalf("New() error = nil, want error containing %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("New() responder = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), "status code 502") {
				t.Errorf("New() error = %q, want status context", err)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("New() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestNewRejectsReservedHeadersCaseInsensitively(t *testing.T) {
	for _, headerName := range []string{
		"connection",
		"CONTENT-LENGTH",
		"Content-Encoding",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"Proxy-Connection",
		"te",
		"Trailer",
		"Transfer-Encoding",
		"Upgrade",
		"x-ReQuEsT-iD",
	} {
		t.Run(headerName, func(t *testing.T) {
			got, err := New(map[int]Response{
				http.StatusBadGateway: {
					Headers: map[string]string{headerName: "value"},
				},
			})
			if err == nil {
				t.Fatal("New() error = nil, want reserved header error")
			}
			if got != nil {
				t.Errorf("New() responder = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), "is not allowed") {
				t.Errorf("New() error = %q, want reserved header context", err)
			}
		})
	}
}

func TestResponderWritesCustomResponse(t *testing.T) {
	responses := map[int]Response{
		http.StatusBadGateway: {
			Body: "  upstream unavailable  ",
			Headers: map[string]string{
				"Content-Type":  "application/json",
				"Cache-Control": "no-store",
			},
		},
	}
	responder, err := New(responses)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	response := responses[http.StatusBadGateway]
	response.Body = "mutated"
	response.Headers["Content-Type"] = "text/plain"
	responses[http.StatusBadGateway] = response
	responses[http.StatusGatewayTimeout] = Response{Body: "timeout"}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://gateway.example/api", nil)
	responder.Write(recorder, request, http.StatusBadGateway)

	if recorder.Code != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if got := recorder.Body.String(); got != "  upstream unavailable  " {
		t.Errorf("body = %q, want %q", got, "  upstream unavailable  ")
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want %q", got, "application/json")
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

func TestResponderFallsBackToStandardHTTPError(t *testing.T) {
	responder, err := New(nil)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://gateway.example/api", nil)
	responder.Write(recorder, request, http.StatusGatewayTimeout)

	if recorder.Code != http.StatusGatewayTimeout {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusGatewayTimeout)
	}
	if got := recorder.Body.String(); got != "Gateway Timeout\n" {
		t.Errorf("body = %q, want %q", got, "Gateway Timeout\n")
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/plain; charset=utf-8" {
		t.Errorf("Content-Type = %q, want %q", got, "text/plain; charset=utf-8")
	}
}

func TestResponderDoesNotWriteCustomBodyForHEAD(t *testing.T) {
	responder, err := New(map[int]Response{
		http.StatusBadGateway: {
			Body:    "upstream unavailable",
			Headers: map[string]string{"Cache-Control": "no-store"},
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodHead, "http://gateway.example/api", nil)
	responder.Write(recorder, request, http.StatusBadGateway)

	if recorder.Code != http.StatusBadGateway {
		t.Errorf("status code = %d, want %d", recorder.Code, http.StatusBadGateway)
	}
	if recorder.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}
