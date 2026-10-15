package upstream

import (
	"net/url"
	"slices"
	"sync"
	"testing"
)

func TestNewEndpointMembershipOwnsSnapshot(t *testing.T) {
	t.Parallel()

	first := url.URL{Scheme: "http", Host: "first.example"}
	second := url.URL{Scheme: "http", Host: "second.example"}
	endpoints := []url.URL{first, second, first}
	membership := NewEndpointMembership(endpoints)
	endpoints[0].Host = "mutated-input.example"

	want := []url.URL{first, second, first}
	got := membership.HealthyEndpoints()
	if !slices.Equal(got, want) {
		t.Fatalf("HealthyEndpoints() = %v, want %v", got, want)
	}
	got[0].Host = "mutated-output.example"
	if next := membership.HealthyEndpoints(); !slices.Equal(next, want) {
		t.Errorf("mutating returned snapshot changed membership to %v", next)
	}
}

func TestEndpointMembershipReplacePublishesSnapshotIncludingEmpty(t *testing.T) {
	t.Parallel()

	initial := url.URL{Scheme: "http", Host: "initial.example"}
	replacement := url.URL{Scheme: "http", Host: "replacement.example"}
	membership := NewEndpointMembership([]url.URL{initial})
	endpoints := []url.URL{replacement, replacement}

	membership.Replace(endpoints)
	endpoints[0].Host = "mutated.example"

	want := []url.URL{replacement, replacement}
	if got := membership.HealthyEndpoints(); !slices.Equal(got, want) {
		t.Errorf("HealthyEndpoints() after Replace() = %v, want %v", got, want)
	}
	membership.Replace(nil)
	if got := membership.HealthyEndpoints(); len(got) != 0 {
		t.Errorf("HealthyEndpoints() after empty Replace() = %v, want empty", got)
	}
}

func TestEndpointMembershipZeroValueIsEmpty(t *testing.T) {
	t.Parallel()

	var membership EndpointMembership

	if got := membership.HealthyEndpoints(); got != nil {
		t.Errorf("zero-value HealthyEndpoints() = %v, want nil", got)
	}
}

func TestEndpointMembershipSupportsConcurrentReplaceAndRead(t *testing.T) {
	t.Parallel()

	first := []url.URL{{Scheme: "http", Host: "first.example"}}
	second := []url.URL{
		{Scheme: "http", Host: "second.example"},
		{Scheme: "http", Host: "third.example"},
	}
	membership := NewEndpointMembership(first)

	const iterations = 1_000
	var waitGroup sync.WaitGroup
	waitGroup.Add(3)
	go func() {
		defer waitGroup.Done()
		for i := range iterations {
			if i%2 == 0 {
				membership.Replace(first)
			} else {
				membership.Replace(second)
			}
		}
	}()
	invalidSnapshot := make(chan []url.URL, 1)
	for range 2 {
		go func() {
			defer waitGroup.Done()
			for range iterations {
				got := membership.HealthyEndpoints()
				if !slices.Equal(got, first) && !slices.Equal(got, second) {
					select {
					case invalidSnapshot <- got:
					default:
					}
					return
				}
			}
		}()
	}
	waitGroup.Wait()
	close(invalidSnapshot)
	if got, exists := <-invalidSnapshot; exists {
		t.Errorf("concurrent read observed partial snapshot: %v", got)
	}
}
