package errorresponse

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// Response describes the body and headers of a custom gateway error response.
type Response struct {
	Body    string
	Headers map[string]string
}

// Responder writes custom gateway errors and falls back to standard HTTP error
// responses for status codes without a custom response.
type Responder struct {
	responses map[int]Response
}

// New validates and copies responses before constructing a Responder.
func New(responses map[int]Response) (*Responder, error) {
	for statusCode, response := range responses {
		if statusCode < 400 || statusCode > 599 {
			return nil, fmt.Errorf("status code %d must be between 400 and 599", statusCode)
		}
		seen := make(map[string]struct{}, len(response.Headers))
		for headerName, headerValue := range response.Headers {
			if !httpguts.ValidHeaderFieldName(headerName) {
				return nil, fmt.Errorf(
					"validate response for status code %d: invalid header name %q",
					statusCode,
					headerName,
				)
			}
			if !httpguts.ValidHeaderFieldValue(headerValue) {
				return nil, fmt.Errorf(
					"validate response for status code %d: invalid value %q for header %q",
					statusCode,
					headerValue,
					headerName,
				)
			}
			normalizedHeaderName := strings.ToLower(headerName)
			switch normalizedHeaderName {
			case "connection",
				"content-length",
				"content-encoding",
				"keep-alive",
				"proxy-authenticate",
				"proxy-authorization",
				"proxy-connection",
				"te",
				"trailer",
				"transfer-encoding",
				"upgrade",
				"x-request-id":
				return nil, fmt.Errorf(
					"validate response for status code %d: header %q is not allowed",
					statusCode,
					headerName,
				)
			}
			if _, exists := seen[normalizedHeaderName]; exists {
				return nil, fmt.Errorf(
					"validate response for status code %d: duplicate header name: %q",
					statusCode,
					headerName,
				)
			}
			seen[normalizedHeaderName] = struct{}{}
		}
	}
	responsesClone := make(map[int]Response, len(responses))
	for statusCode, response := range responses {
		responsesClone[statusCode] = Response{
			Body:    response.Body,
			Headers: maps.Clone(response.Headers),
		}
	}
	return &Responder{
		responses: responsesClone,
	}, nil
}

// Write writes the custom response configured for statusCode or a standard
// HTTP error when no custom response is configured.
func (r *Responder) Write(
	w http.ResponseWriter,
	req *http.Request,
	statusCode int,
) {
	response, ok := r.responses[statusCode]
	if !ok {
		http.Error(
			w,
			http.StatusText(statusCode),
			statusCode,
		)
		return
	}
	for headerName, headerValue := range response.Headers {
		w.Header().Set(headerName, headerValue)
	}
	w.WriteHeader(statusCode)
	if response.Body != "" && req.Method != http.MethodHead {
		_, _ = io.WriteString(w, response.Body)
	}
}
