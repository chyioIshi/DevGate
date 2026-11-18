package main

import (
	"fmt"
	"net/http"

	"github.com/chyioishi/devgate/internal/apikey"
	"github.com/chyioishi/devgate/internal/config"
	"github.com/chyioishi/devgate/internal/router"
)

func newAPIKeyMiddleware(
	next http.Handler,
	policy router.APIKeyAuthenticationPolicy,
	configuredDigests map[string]config.APIKeyDigest,
	errorResponder apikey.ErrorResponder,
	observer apikey.Observer,
) (http.Handler, error) {
	credentials := make(
		[]apikey.Credential,
		0,
		len(policy.Credentials),
	)
	for _, credential := range policy.Credentials {
		credentialDigest, ok := configuredDigests[credential]
		if !ok {
			return nil, fmt.Errorf(
				"digest for credential %q was not found",
				credential,
			)
		}
		digest := apikey.Digest(credentialDigest)
		apiKeyCredential := apikey.Credential{
			Name:   credential,
			Digest: digest,
		}
		credentials = append(credentials, apiKeyCredential)
	}
	middleware, err := apikey.New(
		next,
		policy.Header,
		credentials,
		errorResponder,
		observer,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create API key middleware: %w",
			err,
		)
	}
	return middleware, nil
}
