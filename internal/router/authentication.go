package router

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// AuthenticationPolicy defines the authentication mechanism required by a
// route.
type AuthenticationPolicy struct {
	APIKey *APIKeyAuthenticationPolicy
}

// APIKeyAuthenticationPolicy defines the request header and named credentials
// accepted by API-key authentication.
type APIKeyAuthenticationPolicy struct {
	Header      string
	Credentials []string
}

func (p AuthenticationPolicy) validate() error {
	if p.APIKey == nil {
		return errors.New(
			"API key was not provided",
		)
	}
	if err := p.APIKey.validate(); err != nil {
		return fmt.Errorf(
			"API key policy: %w",
			err,
		)
	}
	return nil
}

func (p APIKeyAuthenticationPolicy) validate() error {
	if strings.TrimSpace(p.Header) == "" {
		return errors.New("header name of API key must not be empty")
	}
	if p.Header != strings.TrimSpace(p.Header) {
		return errors.New(
			"header name of API key must not have leading or trailing whitespace",
		)
	}
	if !httpguts.ValidHeaderFieldName(p.Header) {
		return errors.New(
			"header name of API key must be valid",
		)
	}
	if len(p.Credentials) == 0 {
		return errors.New(
			"credentials of API key must not be empty",
		)
	}
	seen := make(map[string]struct{})
	for _, credential := range p.Credentials {
		if strings.TrimSpace(credential) == "" {
			return errors.New("credential of API key must not be empty")
		}
		if credential != strings.TrimSpace(credential) {
			return fmt.Errorf(
				"credential %q of API key must not have leading or trailing whitespace",
				credential,
			)
		}
		if _, exists := seen[credential]; exists {
			return fmt.Errorf(
				"credential %q is duplicated",
				credential,
			)
		}
		seen[credential] = struct{}{}
	}
	return nil
}
