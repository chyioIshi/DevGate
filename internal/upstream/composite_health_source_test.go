package upstream

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestNewCompositeHealthSourceRejectsInvalidSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sources     []healthyEndpointSource
		wantMessage string
	}{
		{
			name:        "missing sources",
			wantMessage: "no healthy endpoint sources provided",
		},
		{
			name: "nil source",
			sources: []healthyEndpointSource{
				&staticHealthyEndpointSource{},
				nil,
			},
			wantMessage: "nil healthy endpoint source provided",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewCompositeHealthSource(test.sources...)
			if err == nil {
				t.Fatalf("NewCompositeHealthSource() error = nil, want %q", test.wantMessage)
			}
			if got != nil {
				t.Errorf("NewCompositeHealthSource() source = %#v, want nil", got)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Errorf("NewCompositeHealthSource() error = %q, want context %q", err, test.wantMessage)
			}
		})
	}
}

func TestCompositeHealthSourceIntersectsSnapshots(t *testing.T) {
	t.Parallel()

	a := url.URL{Scheme: "http", Host: "a.example"}
	b := url.URL{Scheme: "http", Host: "b.example"}
	c := url.URL{Scheme: "http", Host: "c.example"}
	d := url.URL{Scheme: "http", Host: "d.example"}

	source, err := NewCompositeHealthSource(
		&staticHealthyEndpointSource{endpoints: []url.URL{a, b, b, c}},
		&staticHealthyEndpointSource{endpoints: []url.URL{d, c, b}},
		&staticHealthyEndpointSource{endpoints: []url.URL{b, d}},
	)
	if err != nil {
		t.Fatalf("NewCompositeHealthSource() error = %v", err)
	}

	want := []url.URL{b, b}
	if got := source.HealthyEndpoints(); !slices.Equal(got, want) {
		t.Errorf("HealthyEndpoints() = %v, want %v", got, want)
	}
}

func TestCompositeHealthSourceReturnsEmptyIntersection(t *testing.T) {
	t.Parallel()

	a := url.URL{Scheme: "http", Host: "a.example"}
	b := url.URL{Scheme: "http", Host: "b.example"}
	source, err := NewCompositeHealthSource(
		&staticHealthyEndpointSource{endpoints: []url.URL{a}},
		&staticHealthyEndpointSource{endpoints: []url.URL{b}},
	)
	if err != nil {
		t.Fatalf("NewCompositeHealthSource() error = %v", err)
	}

	if got := source.HealthyEndpoints(); len(got) != 0 {
		t.Errorf("HealthyEndpoints() = %v, want empty snapshot", got)
	}
}

func TestCompositeHealthSourceOwnsSourceListAndResult(t *testing.T) {
	t.Parallel()

	a := url.URL{Scheme: "http", Host: "a.example"}
	b := url.URL{Scheme: "http", Host: "b.example"}
	first := &staticHealthyEndpointSource{endpoints: []url.URL{a}}
	provided := []healthyEndpointSource{first}
	source, err := NewCompositeHealthSource(provided...)
	if err != nil {
		t.Fatalf("NewCompositeHealthSource() error = %v", err)
	}

	provided[0] = &staticHealthyEndpointSource{endpoints: []url.URL{b}}
	got := source.HealthyEndpoints()
	if !slices.Equal(got, []url.URL{a}) {
		t.Fatalf("HealthyEndpoints() after source-list mutation = %v, want %v", got, []url.URL{a})
	}

	got[0] = b
	if next := source.HealthyEndpoints(); !slices.Equal(next, []url.URL{a}) {
		t.Errorf("HealthyEndpoints() after result mutation = %v, want %v", next, []url.URL{a})
	}
}

type staticHealthyEndpointSource struct {
	endpoints []url.URL
}

func (s *staticHealthyEndpointSource) HealthyEndpoints() []url.URL {
	return s.endpoints
}
