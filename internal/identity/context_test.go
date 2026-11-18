package identity

import (
	"context"
	"testing"
)

func TestContextRoundTrip(t *testing.T) {
	parent := context.Background()
	want := Identity{
		Subject: "internal-service",
		Method:  MethodAPIKey,
	}

	ctx := NewContext(parent, want)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext() ok = false, want true")
	}
	if got != want {
		t.Errorf("FromContext() = %+v, want %+v", got, want)
	}
	if _, ok := FromContext(parent); ok {
		t.Error("FromContext(parent) ok = true, want false")
	}
}

func TestFromContextWithoutIdentity(t *testing.T) {
	got, ok := FromContext(context.Background())
	if ok {
		t.Error("FromContext() ok = true, want false")
	}
	if got != (Identity{}) {
		t.Errorf("FromContext() = %+v, want zero value", got)
	}
}
