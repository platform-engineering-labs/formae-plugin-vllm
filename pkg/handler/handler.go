package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

// Handler implements one resource type's operations against a vLLM client.
type Handler interface {
	Create(ctx context.Context, c *vllm.Client, props json.RawMessage) *resource.ProgressResult
	Read(ctx context.Context, c *vllm.Client, nativeID string) *resource.ReadResult
	Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult
	Delete(ctx context.Context, c *vllm.Client, nativeID string) *resource.ProgressResult
	List(ctx context.Context, c *vllm.Client) (*resource.ListResult, error)
}

var registry = map[string]Handler{}

func Register(resourceType string, h Handler) { registry[resourceType] = h }

func For(resourceType string) (Handler, bool) {
	h, ok := registry[resourceType]
	return h, ok
}

// mapError converts a client error into an OperationErrorCode.
// An APIError (server answered) maps by status; a transport failure (no response)
// maps to NetworkFailure/ServiceTimeout — NEVER NotFound. This is what lets the
// agent tell "unreachable" from "deleted".
func mapError(err error) resource.OperationErrorCode {
	if err == nil {
		return ""
	}
	var apiErr *vllm.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 401:
			return resource.OperationErrorCodeInvalidCredentials
		case apiErr.StatusCode == 403:
			return resource.OperationErrorCodeAccessDenied
		case apiErr.StatusCode == 404:
			return resource.OperationErrorCodeNotFound
		case apiErr.StatusCode == 409:
			return resource.OperationErrorCodeAlreadyExists
		case apiErr.StatusCode == 429:
			return resource.OperationErrorCodeThrottling
		case apiErr.StatusCode >= 500:
			return resource.OperationErrorCodeServiceInternalError
		default:
			return resource.OperationErrorCodeInvalidRequest
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return resource.OperationErrorCodeServiceTimeout
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return resource.OperationErrorCodeServiceTimeout
	}
	return resource.OperationErrorCodeNetworkFailure
}

// mapLoadError maps errors from a LoRA *load* (Create/Update). vLLM returns 404
// when the lora_path artifact or base model cannot be found — a permanent user
// error, so it maps to InvalidRequest rather than the recoverable NotFound that
// a bare status-code mapping would give (which would retry a bad path forever).
// On an *unload* the same 404 means "already gone" and is handled as success in
// Delete via mapError, so this distinction lives only on the load path.
func mapLoadError(err error) resource.OperationErrorCode {
	var apiErr *vllm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
		return resource.OperationErrorCodeInvalidRequest
	}
	return mapError(err)
}

// isAlreadyLoaded reports whether a load failed solely because the adapter is
// already loaded (vLLM answers 400). Create reconciles this idempotently by
// reloading in place (spec: "Already-loaded on Create => reconcile via
// load_inplace"). Matches both the real vLLM message and the fake's.
func isAlreadyLoaded(err error) bool {
	var apiErr *vllm.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == 400 {
		b := strings.ToLower(apiErr.Body)
		return strings.Contains(b, "already") && strings.Contains(b, "loaded")
	}
	return false
}

// Success and Fail build ProgressResults (exported for vllm.go dispatch).
func Success(op resource.Operation, nativeID string, props json.RawMessage) *resource.ProgressResult {
	return &resource.ProgressResult{Operation: op, OperationStatus: resource.OperationStatusSuccess, NativeID: nativeID, ResourceProperties: props}
}

func Fail(op resource.Operation, code resource.OperationErrorCode, msg string) *resource.ProgressResult {
	return &resource.ProgressResult{Operation: op, OperationStatus: resource.OperationStatusFailure, ErrorCode: code, StatusMessage: msg}
}
