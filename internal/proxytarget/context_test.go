package proxytarget_test

import (
	"context"
	"net/url"
	"testing"

	"github.com/chyioishi/devgate/internal/proxytarget"
)

func TestTargetContextRoundTrip(t *testing.T) {
	t.Parallel()

	target := url.URL{
		Scheme:   "https",
		Host:     "upstream.example",
		Path:     "/api",
		RawQuery: "version=1",
	}
	ctx := proxytarget.WithTarget(context.Background(), target)

	got, ok := proxytarget.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext() ok = false, want true")
	}
	if got != target {
		t.Errorf("FromContext() target = %v, want %v", got, target)
	}
}

func TestTargetContextStoresValueSnapshot(t *testing.T) {
	t.Parallel()

	target := url.URL{Scheme: "http", Host: "original.example"}
	ctx := proxytarget.WithTarget(context.Background(), target)
	target.Host = "mutated.example"

	got, ok := proxytarget.FromContext(ctx)
	if !ok {
		t.Fatal("FromContext() ok = false, want true")
	}
	if got.Host != "original.example" {
		t.Errorf("FromContext() target host = %q, want %q", got.Host, "original.example")
	}
}

func TestTargetFromContextReportsMissingValue(t *testing.T) {
	t.Parallel()

	got, ok := proxytarget.FromContext(context.Background())
	if ok {
		t.Error("FromContext() ok = true, want false")
	}
	if got != (url.URL{}) {
		t.Errorf("FromContext() target = %v, want zero URL", got)
	}
}

func TestTargetContextDoesNotCollideWithForeignKey(t *testing.T) {
	t.Parallel()

	type contextKey struct{}
	foreignTarget := url.URL{Scheme: "http", Host: "foreign.example"}
	ctx := context.WithValue(context.Background(), contextKey{}, foreignTarget)

	got, ok := proxytarget.FromContext(ctx)
	if ok {
		t.Error("FromContext() ok = true, want false")
	}
	if got != (url.URL{}) {
		t.Errorf("FromContext() target = %v, want zero URL", got)
	}
}
