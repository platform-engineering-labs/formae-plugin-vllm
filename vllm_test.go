//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/platform-engineering-labs/formae-plugin-vllm/internal/fakevllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const adapterType = "VLLM::Inference::LoRAAdapter"
const modelTypeT = "VLLM::Inference::Model"

func newFakeTarget(t *testing.T) (*Plugin, json.RawMessage) {
	t.Helper()
	srv := httptest.NewServer(fakevllm.New("base-model"))
	t.Cleanup(srv.Close)
	tc, _ := json.Marshal(map[string]string{"Type": "vllm", "BaseUrl": srv.URL})
	return &Plugin{}, tc
}

func mustCreate(t *testing.T, p *Plugin, tc json.RawMessage, props map[string]any) {
	t.Helper()
	b, _ := json.Marshal(props)
	res, err := p.Create(context.Background(), &resource.CreateRequest{ResourceType: adapterType, Properties: b, TargetConfig: tc})
	if err != nil {
		t.Fatalf("Create err: %v", err)
	}
	if res.ProgressResult.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status=%v code=%v msg=%s", res.ProgressResult.OperationStatus, res.ProgressResult.ErrorCode, res.ProgressResult.StatusMessage)
	}
}

func TestCreate(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"})
}

// TestCreateIdempotent locks the spec's idempotency guarantee: re-creating an
// already-loaded adapter must succeed (reconcile via load_inplace), not fail.
// Real vLLM answers 400 "already loaded" on the second load.
func TestCreateIdempotent(t *testing.T) {
	p, tc := newFakeTarget(t)
	props := map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"}
	mustCreate(t, p, tc, props)
	mustCreate(t, p, tc, props) // second create must also succeed
}

func TestRead(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"})
	rr, err := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if rr.ErrorCode != "" {
		t.Fatalf("unexpected ErrorCode %v", rr.ErrorCode)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(rr.Properties), &got); err != nil {
		t.Fatal(err)
	}
	if got["loraName"] != "sql" || got["parent"] != "base-model" {
		t.Fatalf("unexpected props: %v", got)
	}
}

// TestReadIncludesRequiredFields guards the discovery path: a resource built
// purely from Read must carry every non-optional schema field, or the agent
// rejects it ("missing required fields: [is3dLoraWeight]") and discovery never
// lands it in inventory. is3dLoraWeight is a non-nullable Boolean (default
// false) that vLLM does not report back, so Read must emit it explicitly.
func TestReadIncludesRequiredFields(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"})
	rr, err := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(rr.Properties), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["is3dLoraWeight"]; !ok {
		t.Fatalf("Read output must include is3dLoraWeight (got keys %v)", keysOf(got))
	}
}

func keysOf(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func TestReadNotFound(t *testing.T) {
	p, tc := newFakeTarget(t)
	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "ghost", ResourceType: adapterType, TargetConfig: tc})
	if rr.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Fatalf("want NotFound, got %v", rr.ErrorCode)
	}
}

func TestReadUnreachable(t *testing.T) {
	p := &Plugin{}
	tc, _ := json.Marshal(map[string]string{"Type": "vllm", "BaseUrl": "http://127.0.0.1:1"})
	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	if rr.ErrorCode == resource.OperationErrorCodeNotFound {
		t.Fatal("unreachable must NOT be NotFound (offline != deleted)")
	}
	if rr.ErrorCode != resource.OperationErrorCodeNetworkFailure && rr.ErrorCode != resource.OperationErrorCodeServiceTimeout {
		t.Fatalf("want NetworkFailure/ServiceTimeout, got %v", rr.ErrorCode)
	}
}

func TestUpdate(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/v1", "baseModelName": "base-model"})
	prior, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/v1"})
	desired, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/v2", "baseModelName": "base-model"})
	res, err := p.Update(context.Background(), &resource.UpdateRequest{NativeID: "sql", ResourceType: adapterType, PriorProperties: prior, DesiredProperties: desired, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProgressResult.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("update failed: %v %s", res.ProgressResult.ErrorCode, res.ProgressResult.StatusMessage)
	}
	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	var got map[string]any
	_ = json.Unmarshal([]byte(rr.Properties), &got)
	if got["loraPath"] != "/p/v2" {
		t.Fatalf("loraPath = %v, want /p/v2", got["loraPath"])
	}
}

func TestDelete(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/sql"})
	res, err := p.Delete(context.Background(), &resource.DeleteRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProgressResult.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("delete failed: %v", res.ProgressResult.ErrorCode)
	}
	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: adapterType, TargetConfig: tc})
	if rr.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Fatalf("want NotFound after delete, got %v", rr.ErrorCode)
	}
}

func TestDeleteNotFound(t *testing.T) {
	p, tc := newFakeTarget(t)
	res, err := p.Delete(context.Background(), &resource.DeleteRequest{NativeID: "ghost", ResourceType: adapterType, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProgressResult.OperationStatus != resource.OperationStatusSuccess {
		t.Fatal("deleting an absent adapter must be success")
	}
}

func TestListAdapters(t *testing.T) {
	p, tc := newFakeTarget(t)
	mustCreate(t, p, tc, map[string]any{"loraName": "sql", "loraPath": "/p/sql"})
	res, err := p.List(context.Background(), &resource.ListRequest{ResourceType: adapterType, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NativeIDs) != 1 || res.NativeIDs[0] != "sql" {
		t.Fatalf("want [sql], got %v", res.NativeIDs)
	}
}

func TestListModelsDiscoversBase(t *testing.T) {
	p, tc := newFakeTarget(t)
	res, err := p.List(context.Background(), &resource.ListRequest{ResourceType: modelTypeT, TargetConfig: tc})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NativeIDs) != 1 || res.NativeIDs[0] != "base-model" {
		t.Fatalf("want [base-model], got %v", res.NativeIDs)
	}
}
