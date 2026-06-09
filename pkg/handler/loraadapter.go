package handler

import (
	"context"
	"encoding/json"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const loraAdapterType = "VLLM::Inference::LoRAAdapter"

func init() { Register(loraAdapterType, &LoRAAdapterHandler{}) }

type LoRAAdapterHandler struct{}

type loraProps struct {
	LoraName      string `json:"loraName"`
	LoraPath      string `json:"loraPath"`
	BaseModelName string `json:"baseModelName,omitempty"`
	// No omitempty: is3dLoraWeight is a non-nullable schema field (Boolean = false).
	// vLLM does not report it in /v1/models, so Read defaults it to false, but it
	// MUST always be emitted or discovery rejects the resource ("missing required
	// fields: [is3dLoraWeight]") and it never lands in inventory.
	Is3DLoraWeight bool   `json:"is3dLoraWeight"`
	ID             string `json:"id,omitempty"`
	Parent         string `json:"parent,omitempty"`
	Root           string `json:"root,omitempty"`
}

func (h *LoRAAdapterHandler) Create(ctx context.Context, c *vllm.Client, raw json.RawMessage) *resource.ProgressResult {
	var p loraProps
	if err := json.Unmarshal(raw, &p); err != nil || p.LoraName == "" || p.LoraPath == "" {
		return Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "loraName and loraPath are required")
	}
	req := vllm.LoadRequest{
		LoraName: p.LoraName, LoraPath: p.LoraPath,
		BaseModelName: p.BaseModelName, Is3DLoraWeight: p.Is3DLoraWeight,
	}
	if err := c.LoadAdapter(ctx, req); err != nil {
		// Idempotency: if the adapter is already loaded, reconcile in place to the
		// desired path instead of failing (spec: already-loaded on Create => reload).
		if isAlreadyLoaded(err) {
			req.LoadInplace = true
			if err2 := c.LoadAdapter(ctx, req); err2 != nil {
				return Fail(resource.OperationCreate, mapLoadError(err2), err2.Error())
			}
		} else {
			return Fail(resource.OperationCreate, mapLoadError(err), err.Error())
		}
	}
	rr := h.Read(ctx, c, p.LoraName)
	if rr.ErrorCode == "" && rr.Properties != "" {
		return Success(resource.OperationCreate, p.LoraName, json.RawMessage(rr.Properties))
	}
	out, _ := json.Marshal(p)
	return Success(resource.OperationCreate, p.LoraName, out)
}

func (h *LoRAAdapterHandler) Read(ctx context.Context, c *vllm.Client, nativeID string) *resource.ReadResult {
	models, err := c.ListModels(ctx)
	if err != nil {
		return &resource.ReadResult{ResourceType: loraAdapterType, ErrorCode: mapError(err)}
	}
	for _, m := range models {
		if m.ID == nativeID && m.Parent != nil {
			out := loraProps{
				LoraName: m.ID, LoraPath: m.Root,
				BaseModelName: *m.Parent,
				ID:            m.ID, Parent: *m.Parent, Root: m.Root,
			}
			b, _ := json.Marshal(out)
			return &resource.ReadResult{ResourceType: loraAdapterType, Properties: string(b)}
		}
	}
	return &resource.ReadResult{ResourceType: loraAdapterType, ErrorCode: resource.OperationErrorCodeNotFound}
}

func (h *LoRAAdapterHandler) Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult {
	var p loraProps
	if err := json.Unmarshal(desired, &p); err != nil || p.LoraPath == "" {
		return Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "loraPath required")
	}
	if err := c.LoadAdapter(ctx, vllm.LoadRequest{
		LoraName: nativeID, LoraPath: p.LoraPath,
		BaseModelName: p.BaseModelName, Is3DLoraWeight: p.Is3DLoraWeight,
		LoadInplace: true,
	}); err != nil {
		return Fail(resource.OperationUpdate, mapLoadError(err), err.Error())
	}
	rr := h.Read(ctx, c, nativeID)
	if rr.ErrorCode == "" {
		return Success(resource.OperationUpdate, nativeID, json.RawMessage(rr.Properties))
	}
	out, _ := json.Marshal(p)
	return Success(resource.OperationUpdate, nativeID, out)
}

func (h *LoRAAdapterHandler) Delete(ctx context.Context, c *vllm.Client, nativeID string) *resource.ProgressResult {
	err := c.UnloadAdapter(ctx, nativeID)
	if err == nil {
		return Success(resource.OperationDelete, nativeID, nil)
	}
	if mapError(err) == resource.OperationErrorCodeNotFound {
		return Success(resource.OperationDelete, nativeID, nil) // already gone
	}
	return Fail(resource.OperationDelete, mapError(err), err.Error())
}

func (h *LoRAAdapterHandler) List(ctx context.Context, c *vllm.Client) (*resource.ListResult, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range models {
		if m.Parent != nil {
			ids = append(ids, m.ID)
		}
	}
	return &resource.ListResult{NativeIDs: ids}, nil
}
