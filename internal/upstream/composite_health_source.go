package upstream

import (
	"errors"
	"net/url"
	"slices"
)

// CompositeHealthSource reports only endpoints considered healthy by every
// configured source.
type CompositeHealthSource struct {
	sources []healthyEndpointSource
}

// NewCompositeHealthSource creates a health source that intersects snapshots
// from the provided sources while preserving the first source's order.
func NewCompositeHealthSource(
	sources ...healthyEndpointSource,
) (*CompositeHealthSource, error) {
	if len(sources) == 0 {
		return nil, errors.New("no healthy endpoint sources provided")
	}
	for _, source := range sources {
		if source == nil {
			return nil, errors.New("nil healthy endpoint source provided")
		}
	}
	return &CompositeHealthSource{
		sources: slices.Clone(sources),
	}, nil
}

// HealthyEndpoints returns the intersection of the current source snapshots.
func (s *CompositeHealthSource) HealthyEndpoints() []url.URL {
	healthyEndpoints := slices.Clone(s.sources[0].HealthyEndpoints())
	for _, source := range s.sources[1:] {
		sourceEndpoints := source.HealthyEndpoints()
		allowedSourceEndpoints := make(map[string]struct{}, len(sourceEndpoints))
		for _, endpoint := range sourceEndpoints {
			allowedSourceEndpoints[endpoint.String()] = struct{}{}
		}

		healthyEndpoints = slices.DeleteFunc(healthyEndpoints, func(endpoint url.URL) bool {
			_, exists := allowedSourceEndpoints[endpoint.String()]
			return !exists
		})
	}
	return healthyEndpoints
}
