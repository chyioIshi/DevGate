package main

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/errorresponse"
)

func globalErrorResponderFromConfig(
	responses map[int]config.ErrorResponseConfig,
) (*errorresponse.Responder, error) {
	responderResponses := make(map[int]errorresponse.Response, len(responses))
	for statusCode, responseConfig := range responses {
		switch statusCode {
		case http.StatusNotFound,
			http.StatusMethodNotAllowed,
			http.StatusInternalServerError:
		default:
			return nil, errors.New("global error response status code must be one of 404, 405, or 500")
		}

		responderResponses[statusCode] = errorresponse.Response{
			Body:    responseConfig.Body,
			Headers: responseConfig.Headers,
		}
	}

	responder, err := errorresponse.New(responderResponses)
	if err != nil {
		return nil, fmt.Errorf("create global error responder: %w", err)
	}
	return responder, nil
}
