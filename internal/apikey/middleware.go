package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/chyioishi/devgate/internal/identity"
)

// Digest is the SHA-256 digest of an API-key secret.
type Digest [sha256.Size]byte

// Credential binds a stable credential name to its API-key digest.
type Credential struct {
	Name   string
	Digest Digest
}

// ErrorResponder writes an HTTP error produced by API-key middleware.
type ErrorResponder func(
	http.ResponseWriter,
	*http.Request,
	int,
)

// Observer records API-key authentication outcomes.
type Observer interface {
	RecordAuthenticatedRequest()
	RecordRejectedRequest()
}

// New creates middleware that authenticates one API key from header against
// the configured credential digests. It removes the API-key header before
// delegating an authenticated request to next.
func New(
	next http.Handler,
	header string,
	credentials []Credential,
	errorResponder ErrorResponder,
	observer Observer,
) (http.Handler, error) {
	if next == nil {
		return nil, errors.New("next handler must not be nil")
	}
	if strings.TrimSpace(header) == "" {
		return nil, errors.New("header must not be empty")
	}
	if header != strings.TrimSpace(header) {
		return nil, errors.New("header must not have leading or trailing whitespace")
	}
	if !httpguts.ValidHeaderFieldName(header) {
		return nil, fmt.Errorf("header %q must be valid", header)
	}
	if len(credentials) == 0 {
		return nil, errors.New("credentials must not be empty")
	}

	seenNames := make(map[string]struct{}, len(credentials))
	seenDigests := make(map[Digest]string, len(credentials))
	for _, credential := range credentials {
		if strings.TrimSpace(credential.Name) == "" {
			return nil, errors.New("credential name must not be empty")
		}
		if credential.Name != strings.TrimSpace(credential.Name) {
			return nil, fmt.Errorf(
				"credential name %q must not have leading or trailing whitespace",
				credential.Name,
			)
		}
		if _, exists := seenNames[credential.Name]; exists {
			return nil, fmt.Errorf(
				"credential name %q is duplicated",
				credential.Name,
			)
		}
		seenNames[credential.Name] = struct{}{}

		if credential.Digest == (Digest{}) {
			return nil, fmt.Errorf(
				"credential %q digest must not be zero",
				credential.Name,
			)
		}
		if existingName, exists := seenDigests[credential.Digest]; exists {
			return nil, fmt.Errorf(
				"credentials %q and %q have the same digest",
				existingName,
				credential.Name,
			)
		}
		seenDigests[credential.Digest] = credential.Name
	}

	credentials = slices.Clone(credentials)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaderValues := r.Header.Values(header)
		if len(requestHeaderValues) != 1 {
			writeUnauthorized(w, r, observer, errorResponder)
			return
		}

		requestDigest := sha256.Sum256([]byte(requestHeaderValues[0]))
		matched := 0
		matchedIndex := 0
		for i, credential := range credentials {
			comparison := subtle.ConstantTimeCompare(
				requestDigest[:],
				credential.Digest[:],
			)
			matched = matched | comparison
			matchedIndex = subtle.ConstantTimeSelect(
				comparison,
				i,
				matchedIndex,
			)
		}
		if matched != 1 {
			writeUnauthorized(w, r, observer, errorResponder)
			return
		}

		if observer != nil {
			observer.RecordAuthenticatedRequest()
		}
		matchedIdentity := identity.Identity{
			Subject: credentials[matchedIndex].Name,
			Method:  identity.MethodAPIKey,
		}
		r = r.WithContext(
			identity.NewContext(r.Context(),
				matchedIdentity),
		)
		r.Header.Del(header)
		next.ServeHTTP(w, r)
	}), nil
}

func writeUnauthorized(
	w http.ResponseWriter,
	r *http.Request,
	observer Observer,
	errorResponder ErrorResponder,
) {
	if observer != nil {
		observer.RecordRejectedRequest()
	}
	w.Header().Set("WWW-Authenticate", "ApiKey")
	if errorResponder != nil {
		errorResponder(w, r, http.StatusUnauthorized)
		return
	}
	http.Error(
		w,
		http.StatusText(http.StatusUnauthorized),
		http.StatusUnauthorized,
	)
}
