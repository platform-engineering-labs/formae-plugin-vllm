package handler

import (
	"context"
	"encoding/json"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const modelType = "VLLM::Inference::Model"

func init() { Register(modelType, &ModelHandler{}) }

// ModelHandler is discovery/read-only: models are set at vLLM startup, not via API.
type ModelHandler struct{}

type modelProps struct {
	ID   string `json:"id"`
	Root string `json:"root,omitempty"`
}

func (h *ModelHandler) Read(ctx context.Context, c *vllm.Client, nativeID string) *resource.ReadResult {
	models, err := c.ListModels(ctx)
	if err != nil {
		return &resource.ReadResult{ResourceType: modelType, ErrorCode: mapError(err)}
	}
	for _, m := range models {
		if m.ID == nativeID && m.Parent == nil {
			b, _ := json.Marshal(modelProps{ID: m.ID, Root: m.Root})
			return &resource.ReadResult{ResourceType: modelType, Properties: string(b)}
		}
	}
	return &resource.ReadResult{ResourceType: modelType, ErrorCode: resource.OperationErrorCodeNotFound}
}

func (h *ModelHandler) List(ctx context.Context, c *vllm.Client) (*resource.ListResult, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range models {
		if m.Parent == nil {
			ids = append(ids, m.ID)
		}
	}
	return &resource.ListResult{NativeIDs: ids}, nil
}

func (h *ModelHandler) Create(ctx context.Context, c *vllm.Client, props json.RawMessage) *resource.ProgressResult {
	return Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
func (h *ModelHandler) Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult {
	return Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
func (h *ModelHandler) Delete(ctx context.Context, c *vllm.Client, nativeID string) *resource.ProgressResult {
	return Fail(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
