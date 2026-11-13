package config

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/http/httpguts"
)

// APIKeyDigest is the SHA-256 digest of an API-key secret retained at runtime.
type APIKeyDigest [sha256.Size]byte

func (c fileConfig) validateAuthentication() error {
	for keyName, keyValue := range c.APIKeys {
		if strings.TrimSpace(keyName) == "" {
			return errors.New("credential name must not be empty")
		}
		if keyName != strings.TrimSpace(keyName) {
			return fmt.Errorf(
				"credential name %q must not have leading or trailing whitespace",
				keyName,
			)
		}
		if strings.TrimSpace(keyValue.ValueEnv) == "" {
			return fmt.Errorf(
				"environment variable name for API key %q must not be empty",
				keyName,
			)
		}
		if keyValue.ValueEnv != strings.TrimSpace(keyValue.ValueEnv) {
			return fmt.Errorf(
				"environment variable name for API key %q must not have leading or trailing whitespace",
				keyName,
			)
		}
		if !strings.HasPrefix(keyValue.ValueEnv, "DEVGATE_") {
			return fmt.Errorf(
				"environment variable name for API key %q must start with DEVGATE_ prefix",
				keyName,
			)
		}
	}
	for _, route := range c.Routes {
		if route.Authentication == nil {
			continue
		}
		if route.Authentication.APIKey == nil {
			return fmt.Errorf(
				"api key of route %q is not provided",
				route.Name,
			)
		}
		apiKeyHeader := route.Authentication.APIKey.Header
		if strings.TrimSpace(apiKeyHeader) == "" {
			return fmt.Errorf(
				"api key header of route %q must not be empty",
				route.Name,
			)
		}
		if !httpguts.ValidHeaderFieldName(apiKeyHeader) {
			return fmt.Errorf(
				"api key header of route %q must be valid",
				route.Name,
			)
		}

		apiKeyCredentials := route.Authentication.APIKey.Credentials

		if len(apiKeyCredentials) == 0 {
			return fmt.Errorf(
				"api key credentials of route %q must not be empty",
				route.Name,
			)
		}

		seen := make(map[string]struct{}, len(apiKeyCredentials))
		for _, credential := range apiKeyCredentials {
			if strings.TrimSpace(credential) == "" {
				return fmt.Errorf(
					"credential of route %q must not be empty",
					route.Name,
				)
			}
			if credential != strings.TrimSpace(credential) {
				return fmt.Errorf(
					"credential %q of route %q must not have leading or trailing whitespace",
					credential,
					route.Name,
				)
			}
			if _, exists := seen[credential]; exists {
				return fmt.Errorf(
					"credential %q of route %q is duplicated",
					credential,
					route.Name,
				)
			}
			seen[credential] = struct{}{}
			if _, exists := c.APIKeys[credential]; !exists {
				return fmt.Errorf(
					"credential %q of route %q must be in api_keys config",
					credential,
					route.Name,
				)
			}
		}
	}
	return nil
}

func resolveAPIKeys(
	definitions map[string]APIKeyCredentialConfig,
	lookupEnv func(string) (string, bool),
) (map[string]APIKeyDigest, error) {
	if len(definitions) == 0 {
		return nil, nil
	}
	apiKeyDigests := make(
		map[string]APIKeyDigest,
		len(definitions),
	)
	digestOwner := make(
		map[APIKeyDigest]string,
		len(definitions),
	)
	for credentialName, definition := range definitions {
		definitionEnvValue, ok := lookupEnv(definition.ValueEnv)
		if !ok {
			return nil, fmt.Errorf(
				"environment value of api key %q was not defined",
				credentialName,
			)
		}
		if strings.TrimSpace(definitionEnvValue) == "" {
			return nil, fmt.Errorf(
				"environment value of api key %q must not be empty",
				credentialName,
			)
		}
		if definitionEnvValue != strings.TrimSpace(definitionEnvValue) {
			return nil, fmt.Errorf(
				"environment value of api key %q must not have leading or trailing whitespace",
				credentialName,
			)
		}
		if !httpguts.ValidHeaderFieldValue(definitionEnvValue) {
			return nil, fmt.Errorf(
				"header value of api key %q must be valid",
				credentialName,
			)
		}

		digest := sha256.Sum256([]byte(definitionEnvValue))
		if existingCredential, exists := digestOwner[digest]; exists {
			return nil, fmt.Errorf(
				"API key credentials %q and %q resolve to the same secret",
				credentialName,
				existingCredential,
			)
		}
		digestOwner[digest] = credentialName
		apiKeyDigests[credentialName] = digest
	}
	return apiKeyDigests, nil
}
