package handler

import (
	"testing"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

// A 404 means opposite things depending on the operation: on an *unload* it is
// "already gone" (success), but on a *load* it is "bad lora_path / base model"
// — a permanent user error. mapLoadError must classify a load 404 as a permanent
// InvalidRequest, never a recoverable NotFound (which would retry a bad path
// forever instead of surfacing it).
func TestMapLoadError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want resource.OperationErrorCode
	}{
		{"load 404 bad path -> permanent", &vllm.APIError{StatusCode: 404, Body: "No adapter found for /bad"}, resource.OperationErrorCodeInvalidRequest},
		{"500 delegates to mapError", &vllm.APIError{StatusCode: 500}, resource.OperationErrorCodeServiceInternalError},
		{"401 delegates to mapError", &vllm.APIError{StatusCode: 401}, resource.OperationErrorCodeInvalidCredentials},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		if got := mapLoadError(c.err); got != c.want {
			t.Errorf("%s: mapLoadError = %q, want %q", c.name, got, c.want)
		}
	}
}

// isAlreadyLoaded recognises vLLM's "adapter already loaded" 400 so Create can
// reconcile idempotently via load_inplace (spec: already-loaded on Create =>
// reload in place). It must match both the real vLLM message and the fake's.
func TestIsAlreadyLoaded(t *testing.T) {
	yes := []*vllm.APIError{
		{StatusCode: 400, Body: "The lora adapter 'x' has already been loaded. If you want to load the adapter in place, set 'load_inplace' to True."},
		{StatusCode: 400, Body: "adapter already loaded"},
	}
	for _, e := range yes {
		if !isAlreadyLoaded(e) {
			t.Errorf("expected already-loaded for body %q", e.Body)
		}
	}
	no := []error{
		&vllm.APIError{StatusCode: 400, Body: "invalid request"},
		&vllm.APIError{StatusCode: 404, Body: "already been loaded"}, // 404 is bad path, not already-loaded
		nil,
	}
	for _, e := range no {
		if isAlreadyLoaded(e) {
			t.Errorf("did not expect already-loaded for %v", e)
		}
	}
}
