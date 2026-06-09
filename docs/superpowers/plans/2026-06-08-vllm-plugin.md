# vLLM formae Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A formae resource plugin that manages vLLM LoRA adapters (full CRUD) and discovers served models (read-only) against a running vLLM OpenAI-compatible server.

**Architecture:** Runtime/serving plugin. `target = an OpenAI-compatible base_url`. The plugin issues HTTP calls (`GET /v1/models`, `POST /v1/load_lora_adapter`, `POST /v1/unload_lora_adapter`). It follows the `formae-plugin-grafana` prior art: a `Plugin` struct dispatching to per-type handlers via a registry, a cached HTTP client per target, and an error mapper. A **fake vLLM** (in-process `httptest` + standalone binary) backs all unit/integration/conformance tests; real-model e2e runs on the local RTX 5090 and AWS.

**Tech Stack:** Go 1.26, formae plugin SDK (`pkg/plugin`, `pkg/plugin/resource`, `pkg/plugin/sdk`, `pkg/plugin-conformance-tests`), PKL schema, `net/http`, `testify`, Docker, NVIDIA Container Toolkit.

**Source spec:** `docs/superpowers/specs/2026-06-05-vllm-plugin-design.md`
**Verified SDK reference:** memory `formae-notfound-sync-reconcile` + the grafana/template plugins under `/home/jeroen/dev/pel/`.

> **Commit policy (overrides the template's auto-commit):** per this user's global rule, NEVER commit autonomously. Each "Commit" step means *stage and request the user's `/commit` approval*. Do not run `git commit` yourself.

> **MANDATORY before every conformance/e2e run:** `make install` (tests run against the installed binary at `~/.pel/formae/plugins/vllm/v<version>/`, not the source).

---

## File Structure

```
formae-plugin-vllm/
├── main.go                       # sdk.RunWithManifest(&Plugin{}, sdk.RunConfig{})   [scaffolded]
├── plugin.go                     # Plugin struct: config methods + CRUD dispatch to handlers
├── formae-plugin.pkl             # name=vllm namespace=VLLM ...                       [scaffolded]
├── go.mod / go.sum               # [scaffolded]
├── Makefile                      # [scaffolded] + fake-vllm test-env targets
├── schema/
│   ├── Config.pkl                # plugin config: type="vllm"
│   └── pkl/
│       ├── loraadapter.pkl       # VLLM::Inference::LoRAAdapter
│       └── model.pkl             # VLLM::Inference::Model (discovery/read-only)
├── pkg/
│   ├── config/config.go          # TargetConfig{BaseURL}; ParseTargetConfig; APIKey() from env
│   ├── vllm/client.go            # HTTP client: ListModels, LoadAdapter, UnloadAdapter; APIError
│   └── handler/
│       ├── handler.go            # registry, SuccessResult/FailResult, mapError
│       ├── loraadapter.go        # LoRAAdapter CRUD + List
│       └── model.go              # Model Read + List (discovery-only)
├── internal/fakevllm/
│   └── server.go                 # in-memory fake vLLM (http.Handler) for tests
├── cmd/fake-vllm/main.go         # standalone fake binary for conformance
├── vllm_test.go                  # //go:build integration — TDD CRUD against in-process fake
├── conformance_test.go           # //go:build conformance — RunCRUDTests + RunDiscoveryTests [scaffolded, edited]
├── testdata/
│   ├── loraadapter.pkl           # conformance CRUD fixture (Stack+Target+LoRAAdapter)
│   ├── loraadapter-update.pkl    # mutates loraPath
│   └── loraadapter-replace.pkl   # mutates loraName (CreateOnly → replacement)
├── examples/
│   ├── local/                    # docker-compose + forma
│   ├── aws/                      # forma dogfooding AWS plugin + forma
│   └── kubernetes/               # vLLM Deployment+Service + forma targeting the Service
└── README.md
```

---

## Task 0: Scaffold the plugin & verify it builds

**Files:** whole repo (scaffolded). The repo currently contains only `docs/`.

- [ ] **Step 1: Scaffold into the existing directory**

Run from `/home/jeroen/dev/pel/formae-plugin-vllm`:
```bash
formae plugin init --no-input \
  --name vllm --namespace VLLM \
  --description "Manage vLLM LoRA adapters and discover served models" \
  --author "Platform Engineering Labs" \
  --module-path "github.com/platform-engineering-labs/formae-plugin-vllm" \
  --license Apache-2.0
```

- [ ] **Step 2: Inspect what was generated and where**

Run: `ls -R` (exclude `docs/`).
Expected: `main.go`, `plugin.go`, `formae-plugin.pkl`, `go.mod`, `Makefile`, `schema/pkl/`, `conformance_test.go`, `testdata/`, `examples/`.
If init created a *subdirectory* (e.g. `./vllm/`) instead of populating the cwd, move its contents up into the repo root (`mv vllm/* vllm/.* . 2>/dev/null; rmdir vllm`) so the plugin root == repo root and `docs/` sits alongside. Re-run `ls -R` to confirm.

- [ ] **Step 3: Initialize git (no commit)**

Run: `git init && git add -A && git status --short`
Expected: repo initialized, files staged. **Do not commit** — await the user's `/commit`.

- [ ] **Step 4: Confirm it builds**

Run: `make build`
Expected: `bin/vllm` produced; `minFormaeVersion` in `formae-plugin.pkl` auto-updated; exit 0.

- [ ] **Step 5: Run `/init` for full template context**

Invoke the `/init` skill so the agent has the scaffolded template in context before editing.

- [ ] **Step 6: Commit checkpoint**

Stage all and request `/commit`: "chore: scaffold vllm plugin".

> **GUIDED CHECKPOINT:** present the scaffold layout for review before continuing.

---

## Task 1: Schema — LoRAAdapter & Model PKL

**Files:**
- Create: `schema/pkl/loraadapter.pkl`
- Create: `schema/pkl/model.pkl`
- Reference: the scaffolded `schema/pkl/example.pkl` (copy its module/import preamble verbatim) and `/home/jeroen/dev/pel/formae-plugin-grafana/schema/pkl/core/datasource.pkl`.
- Delete: `schema/pkl/example.pkl` (after the two real ones verify).

- [ ] **Step 1: Write `loraadapter.pkl`**

Keep the exact module header / `import` lines from the scaffolded `example.pkl`; replace the class with:
```pkl
@formae.ResourceHint {
  type = "VLLM::Inference::LoRAAdapter"
  identifier = "$.loraName"
}
class LoRAAdapter extends formae.Resource {
  fixed hidden type: String = "VLLM::Inference::LoRAAdapter"

  /// Adapter name; also the model id you call at inference time. Immutable.
  @formae.FieldHint { createOnly = true }
  loraName: String

  /// Filesystem path to the adapter on the vLLM node. Mutable (reload in place).
  @formae.FieldHint {}
  loraPath: String

  /// Base model this adapter attaches to. Immutable.
  @formae.FieldHint { createOnly = true }
  baseModelName: String?

  /// MoE 3D weight layout. Immutable.
  @formae.FieldHint { createOnly = true }
  is3dLoraWeight: Boolean = false

  /// Read-back model id (provider-populated).
  @formae.FieldHint { hasProviderDefault = true }
  id: String?

  /// Read-back base model id (provider-populated).
  @formae.FieldHint { hasProviderDefault = true }
  parent: String?

  /// Read-back artifact location (provider-populated).
  @formae.FieldHint { hasProviderDefault = true }
  root: String?
}
```

- [ ] **Step 2: Write `model.pkl`**

Same preamble; class:
```pkl
@formae.ResourceHint {
  type = "VLLM::Inference::Model"
  identifier = "$.id"
}
class Model extends formae.Resource {
  fixed hidden type: String = "VLLM::Inference::Model"

  /// Served base model id (provider-populated; discovery-only).
  @formae.FieldHint { hasProviderDefault = true }
  id: String?

  /// Artifact location (provider-populated).
  @formae.FieldHint { hasProviderDefault = true }
  root: String?
}
```

- [ ] **Step 3: Remove the example resource**

Run: `rm schema/pkl/example.pkl`

- [ ] **Step 4: Verify the schema**

Run: `make verify-schema`
Expected: PASS for namespace `VLLM`, both resource types recognized. If it reports a missing preamble/import, copy the exact header from another working plugin's pkl (grafana) and re-run.

- [ ] **Step 5: Build**

Run: `make build`
Expected: exit 0.

- [ ] **Step 6: Commit checkpoint** — "feat: add LoRAAdapter and Model PKL schema".

> **GUIDED CHECKPOINT:** present PKL definitions for review.

---

## Task 2: Target config (`pkg/config`)

**Files:**
- Create: `pkg/config/config.go`
- Create: `schema/Config.pkl`
- Test: `pkg/config/config_test.go`

- [ ] **Step 1: Write the failing test**

`pkg/config/config_test.go`:
```go
package config

import "testing"

func TestParseTargetConfig(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","BaseUrl":"http://node:8000"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "http://node:8000" {
		t.Fatalf("BaseURL = %q, want http://node:8000", cfg.BaseURL)
	}
}

func TestParseTargetConfigMissingURL(t *testing.T) {
	if _, err := ParseTargetConfig([]byte(`{"Type":"vllm"}`)); err == nil {
		t.Fatal("expected error for missing BaseUrl")
	}
}
```

- [ ] **Step 2: Run it, expect FAIL** (undefined `ParseTargetConfig`)

Run: `go test ./pkg/config/ -run TestParseTargetConfig -v`
Expected: build/compile failure — `ParseTargetConfig` undefined.

- [ ] **Step 3: Implement**

`pkg/config/config.go`:
```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// TargetConfig is the per-node connection config carried in the forma Target.
type TargetConfig struct {
	Type    string `json:"Type"`
	BaseURL string `json:"BaseUrl"`
}

// ParseTargetConfig decodes the JSON target config and validates BaseURL.
func ParseTargetConfig(data json.RawMessage) (*TargetConfig, error) {
	var cfg TargetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid target config: %w", err)
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("target config missing 'BaseUrl'")
	}
	return &cfg, nil
}

// APIKey returns the optional bearer token from the environment. Empty = no auth.
// Secrets are kept out of the forma file (mirrors grafana's GRAFANA_AUTH convention).
func APIKey() string {
	return os.Getenv("VLLM_API_KEY")
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./pkg/config/ -v`
Expected: PASS.

- [ ] **Step 5: Write `schema/Config.pkl`**

Mirror grafana's `schema/Config.pkl`:
```pkl
open module vllm.Config

extends "formae:/Config.pkl"

class PluginConfig extends BaseResourcePluginConfig {
    type = "vllm"
}
```

- [ ] **Step 6: Commit checkpoint** — "feat: vLLM target config".

> **GUIDED CHECKPOINT:** present target config + auth approach (BaseUrl in target, api_key via `VLLM_API_KEY`).

---

## Task 3: vLLM HTTP client (`pkg/vllm`)

**Files:**
- Create: `pkg/vllm/client.go`
- Test: `pkg/vllm/client_test.go`

- [ ] **Step 1: Write the failing test (against httptest)**

`pkg/vllm/client_test.go`:
```go
package vllm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
		  {"id":"base","object":"model","root":"base","parent":null},
		  {"id":"sql","object":"model","root":"/p/sql","parent":"base"}]}`))
	}))
	defer srv.Close()

	models, err := New(srv.URL, "").ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 2 || models[1].ID != "sql" || models[1].Parent == nil || *models[1].Parent != "base" {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestListModelsTransportError(t *testing.T) {
	// Unroutable port → transport error (no HTTP response).
	_, err := New("http://127.0.0.1:1/", "").ListModels(context.Background())
	if err == nil {
		t.Fatal("expected transport error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport error should NOT be an APIError, got %v", err)
	}
}
```

- [ ] **Step 2: Run it, expect FAIL** (undefined `New`/`APIError`)

Run: `go test ./pkg/vllm/ -v`
Expected: compile failure.

- [ ] **Step 3: Implement the client**

`pkg/vllm/client.go`:
```go
package vllm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Model mirrors an entry in GET /v1/models.
type Model struct {
	ID     string  `json:"id"`
	Object string  `json:"object"`
	Root   string  `json:"root"`
	Parent *string `json:"parent"`
}

type modelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// LoadRequest is the body of POST /v1/load_lora_adapter.
type LoadRequest struct {
	LoraName       string `json:"lora_name"`
	LoraPath       string `json:"lora_path"`
	BaseModelName  string `json:"base_model_name,omitempty"`
	Is3DLoraWeight bool   `json:"is_3d_lora_weight,omitempty"`
	LoadInplace    bool   `json:"load_inplace,omitempty"`
}

// APIError is a non-2xx HTTP response (the server answered). Transport failures
// are returned as the raw error and are NOT APIErrors — that distinction is how
// the handler tells "unreachable" from "not found".
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vllm api error: status %d: %s", e.StatusCode, e.Body)
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err // transport failure — NOT an APIError
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: string(data)}
	}
	return data, nil
}

func (c *Client) ListModels(ctx context.Context) ([]Model, error) {
	data, err := c.do(ctx, http.MethodGet, "/v1/models", nil)
	if err != nil {
		return nil, err
	}
	var mr modelsResponse
	if err := json.Unmarshal(data, &mr); err != nil {
		return nil, err
	}
	return mr.Data, nil
}

func (c *Client) LoadAdapter(ctx context.Context, req LoadRequest) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/load_lora_adapter", req)
	return err
}

func (c *Client) UnloadAdapter(ctx context.Context, loraName string) error {
	_, err := c.do(ctx, http.MethodPost, "/v1/unload_lora_adapter",
		map[string]string{"lora_name": loraName})
	return err
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./pkg/vllm/ -v`
Expected: PASS.

- [ ] **Step 5: Commit checkpoint** — "feat: vLLM HTTP client".

---

## Task 4: Fake vLLM server (`internal/fakevllm` + `cmd/fake-vllm`)

**Files:**
- Create: `internal/fakevllm/server.go`
- Create: `internal/fakevllm/server_test.go`
- Create: `cmd/fake-vllm/main.go`

- [ ] **Step 1: Write the failing test**

`internal/fakevllm/server_test.go`:
```go
package fakevllm

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoadListUnload(t *testing.T) {
	srv := httptest.NewServer(New("base-model"))
	defer srv.Close()
	c := srv.Client()

	// initially only the base model
	body := get(t, c, srv.URL+"/v1/models")
	if strings.Contains(body, "sql") {
		t.Fatal("adapter present before load")
	}
	// load
	post(t, c, srv.URL+"/v1/load_lora_adapter", `{"lora_name":"sql","lora_path":"/p/sql"}`, 200)
	body = get(t, c, srv.URL+"/v1/models")
	if !strings.Contains(body, `"id":"sql"`) || !strings.Contains(body, `"parent":"base-model"`) {
		t.Fatalf("adapter not listed: %s", body)
	}
	// unload
	post(t, c, srv.URL+"/v1/unload_lora_adapter", `{"lora_name":"sql"}`, 200)
	body = get(t, c, srv.URL+"/v1/models")
	if strings.Contains(body, `"id":"sql"`) {
		t.Fatalf("adapter still listed after unload: %s", body)
	}
	// unload again → 404 (not found)
	post(t, c, srv.URL+"/v1/unload_lora_adapter", `{"lora_name":"sql"}`, 404)
}

func get(t *testing.T, c *http.Client, url string) string {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	return string(b[:n])
}

func post(t *testing.T, c *http.Client, url, body string, want int) {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("POST %s → %d, want %d", url, resp.StatusCode, want)
	}
}
```

- [ ] **Step 2: Run it, expect FAIL** (undefined `New`)

Run: `go test ./internal/fakevllm/ -v`
Expected: compile failure.

- [ ] **Step 3: Implement the fake**

`internal/fakevllm/server.go`:
```go
package fakevllm

import (
	"encoding/json"
	"net/http"
	"sync"
)

type adapter struct {
	path   string
	parent string
}

// Server is an in-memory fake vLLM OpenAI server implementing just the endpoints
// the plugin uses, faithful to documented vLLM responses.
type Server struct {
	mu       sync.Mutex
	base     string
	adapters map[string]adapter
}

// New returns an http.Handler serving one base model named baseModel.
func New(baseModel string) *Server {
	return &Server{base: baseModel, adapters: map[string]adapter{}}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/models":
		s.listModels(w)
	case "/v1/load_lora_adapter":
		s.load(w, r)
	case "/v1/unload_lora_adapter":
		s.unload(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) listModels(w http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type model struct {
		ID     string  `json:"id"`
		Object string  `json:"object"`
		Root   string  `json:"root"`
		Parent *string `json:"parent"`
	}
	out := struct {
		Object string  `json:"object"`
		Data   []model `json:"data"`
	}{Object: "list"}
	out.Data = append(out.Data, model{ID: s.base, Object: "model", Root: s.base})
	for name, a := range s.adapters {
		p := a.parent
		out.Data = append(out.Data, model{ID: name, Object: "model", Root: a.path, Parent: &p})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) load(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoraName      string `json:"lora_name"`
		LoraPath      string `json:"lora_path"`
		BaseModelName string `json:"base_model_name"`
		LoadInplace   bool   `json:"load_inplace"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoraName == "" || req.LoraPath == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.adapters[req.LoraName]; exists && !req.LoadInplace {
		http.Error(w, "adapter already loaded", http.StatusBadRequest)
		return
	}
	parent := req.BaseModelName
	if parent == "" {
		parent = s.base
	}
	s.adapters[req.LoraName] = adapter{path: req.LoraPath, parent: parent}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Success: LoRA adapter '" + req.LoraName + "' added successfully"))
}

func (s *Server) unload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoraName string `json:"lora_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.LoraName == "" {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.adapters[req.LoraName]; !ok {
		http.Error(w, "adapter not found", http.StatusNotFound)
		return
	}
	delete(s.adapters, req.LoraName)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Success: LoRA adapter '" + req.LoraName + "' removed successfully"))
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./internal/fakevllm/ -v`
Expected: PASS.

- [ ] **Step 5: Standalone binary for conformance**

`cmd/fake-vllm/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/platform-engineering-labs/formae-plugin-vllm/internal/fakevllm"
)

func main() {
	addr := os.Getenv("FAKE_VLLM_ADDR")
	if addr == "" {
		addr = ":8000"
	}
	base := os.Getenv("FAKE_VLLM_BASE_MODEL")
	if base == "" {
		base = "Qwen/Qwen2.5-0.5B-Instruct"
	}
	log.Printf("fake vLLM on %s (base=%s)", addr, base)
	log.Fatal(http.ListenAndServe(addr, fakevllm.New(base)))
}
```

- [ ] **Step 6: Commit checkpoint** — "test: fake vLLM server + standalone binary".

---

## Task 5: Handler registry, error mapper, plugin dispatch

**Files:**
- Create: `pkg/handler/handler.go`
- Modify: `plugin.go` (CRUD methods dispatch to handlers; add client cache)
- Test: `pkg/handler/handler_test.go`

- [ ] **Step 1: Write the failing test for `mapError`**

`pkg/handler/handler_test.go`:
```go
package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

func TestMapError(t *testing.T) {
	cases := []struct {
		err  error
		want resource.OperationErrorCode
	}{
		{nil, ""},
		{&vllm.APIError{StatusCode: 404}, resource.OperationErrorCodeNotFound},
		{&vllm.APIError{StatusCode: 401}, resource.OperationErrorCodeInvalidCredentials},
		{&vllm.APIError{StatusCode: 403}, resource.OperationErrorCodeAccessDenied},
		{&vllm.APIError{StatusCode: 429}, resource.OperationErrorCodeThrottling},
		{&vllm.APIError{StatusCode: 500}, resource.OperationErrorCodeServiceInternalError},
		{context.DeadlineExceeded, resource.OperationErrorCodeServiceTimeout},
		{errors.New("dial tcp: connection refused"), resource.OperationErrorCodeNetworkFailure},
	}
	for _, c := range cases {
		if got := mapError(c.err); got != c.want {
			t.Errorf("mapError(%v) = %q, want %q", c.err, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it, expect FAIL**

Run: `go test ./pkg/handler/ -v`
Expected: compile failure (undefined `mapError`).

- [ ] **Step 3: Implement `handler.go`**

```go
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net"

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
// CRITICAL: an APIError (the server answered) maps by status; a transport
// failure (no response) maps to NetworkFailure/ServiceTimeout — never NotFound.
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
	// Transport failure (no HTTP response).
	if errors.Is(err, context.DeadlineExceeded) {
		return resource.OperationErrorCodeServiceTimeout
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return resource.OperationErrorCodeServiceTimeout
	}
	return resource.OperationErrorCodeNetworkFailure
}

// Success and Fail are exported so plugin.go can build dispatch-level results too.
func Success(op resource.Operation, nativeID string, props json.RawMessage) *resource.ProgressResult {
	return &resource.ProgressResult{Operation: op, OperationStatus: resource.OperationStatusSuccess, NativeID: nativeID, ResourceProperties: props}
}

func Fail(op resource.Operation, code resource.OperationErrorCode, msg string) *resource.ProgressResult {
	return &resource.ProgressResult{Operation: op, OperationStatus: resource.OperationStatusFailure, ErrorCode: code, StatusMessage: msg}
}
```

- [ ] **Step 4: Run tests, expect PASS**

Run: `go test ./pkg/handler/ -v`
Expected: PASS.

- [ ] **Step 5: Wire `plugin.go` to the registry**

Keep the scaffolded `RateLimit()`, `DiscoveryFilters()`, `LabelConfig()` methods as generated (adjust `RateLimit` to a modest value if the struct exposes an obvious requests-per-second field; otherwise leave the scaffold default). Replace the CRUD method bodies with dispatch. Add a per-target client cache:
```go
// plugin.go (imports: context, encoding/json, fmt, sync;
//   pkg/config, pkg/vllm, pkg/handler; formae .../resource)

type Plugin struct {
	mu      sync.Mutex
	clients map[string]*vllm.Client
}

func (p *Plugin) client(targetConfig json.RawMessage) (*vllm.Client, error) {
	cfg, err := config.ParseTargetConfig(targetConfig)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.clients == nil {
		p.clients = map[string]*vllm.Client{}
	}
	key := string(targetConfig)
	if c, ok := p.clients[key]; ok {
		return c, nil
	}
	c := vllm.New(cfg.BaseURL, config.APIKey())
	p.clients[key] = c
	return c, nil
}

func (p *Plugin) Create(ctx context.Context, req *resource.CreateRequest) (*resource.CreateResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.CreateResult{ProgressResult: handler.Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "unknown resource type "+req.ResourceType)}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.CreateResult{ProgressResult: handler.Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, err.Error())}, nil
	}
	return &resource.CreateResult{ProgressResult: h.Create(ctx, c, req.Properties)}, nil
}

func (p *Plugin) Read(ctx context.Context, req *resource.ReadRequest) (*resource.ReadResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	return h.Read(ctx, c, req.NativeID), nil
}

func (p *Plugin) Update(ctx context.Context, req *resource.UpdateRequest) (*resource.UpdateResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.UpdateResult{ProgressResult: handler.Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "unknown resource type")}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.UpdateResult{ProgressResult: handler.Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error())}, nil
	}
	return &resource.UpdateResult{ProgressResult: h.Update(ctx, c, req.NativeID, req.PriorProperties, req.DesiredProperties)}, nil
}

func (p *Plugin) Delete(ctx context.Context, req *resource.DeleteRequest) (*resource.DeleteResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.DeleteResult{ProgressResult: handler.Fail(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, "unknown resource type")}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.DeleteResult{ProgressResult: handler.Fail(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, err.Error())}, nil
	}
	return &resource.DeleteResult{ProgressResult: h.Delete(ctx, c, req.NativeID)}, nil
}

func (p *Plugin) List(ctx context.Context, req *resource.ListRequest) (*resource.ListResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.ListResult{}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.ListResult{}, nil
	}
	return h.List(ctx, c)
}

func (p *Plugin) Status(ctx context.Context, req *resource.StatusRequest) (*resource.StatusResult, error) {
	// All vLLM ops are synchronous; Status should never be called.
	return &resource.StatusResult{ProgressResult: handler.Fail(resource.OperationCheckStatus, resource.OperationErrorCodeInvalidRequest, "status not supported")}, nil
}
```
`handler.Success`/`handler.Fail` are already exported (Task 5 Step 3), so `plugin.go` can call them directly.

- [ ] **Step 6: Build**

Run: `make build`
Expected: exit 0 (handlers registered in Task 6+; dispatch returns "unknown resource type" until then).

- [ ] **Step 7: Commit checkpoint** — "feat: handler registry, error mapper, CRUD dispatch".

---

## Task 6: LoRAAdapter **Create** (TDD)

**Files:**
- Create: `pkg/handler/loraadapter.go`
- Create/extend: `vllm_test.go` (integration, in-process fake)

- [ ] **Step 1: Write the failing integration test**

`vllm_test.go`:
```go
//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/platform-engineering-labs/formae-plugin-vllm/internal/fakevllm"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFakeTarget(t *testing.T) (*Plugin, json.RawMessage) {
	t.Helper()
	srv := httptest.NewServer(fakevllm.New("base-model"))
	t.Cleanup(srv.Close)
	tc, _ := json.Marshal(map[string]string{"Type": "vllm", "BaseUrl": srv.URL})
	return &Plugin{}, tc
}

func TestCreate(t *testing.T) {
	p, tc := newFakeTarget(t)
	props, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"})
	res, err := p.Create(context.Background(), &resource.CreateRequest{
		ResourceType: "VLLM::Inference::LoRAAdapter", Label: "sql", Properties: props, TargetConfig: tc,
	})
	require.NoError(t, err)
	require.NotNil(t, res.ProgressResult)
	assert.Equal(t, resource.OperationStatusSuccess, res.ProgressResult.OperationStatus)
	assert.Equal(t, "sql", res.ProgressResult.NativeID)
}
```

- [ ] **Step 2: Run it, expect FAIL** (handler not registered → "unknown resource type")

Run: `go test -tags=integration -run TestCreate -v .`
Expected: FAIL — status Failure / "unknown resource type".

- [ ] **Step 3: Implement Create in `loraadapter.go`**

```go
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
	LoraName       string `json:"loraName"`
	LoraPath       string `json:"loraPath"`
	BaseModelName  string `json:"baseModelName,omitempty"`
	Is3DLoraWeight bool   `json:"is3dLoraWeight,omitempty"`
	ID             string `json:"id,omitempty"`
	Parent         string `json:"parent,omitempty"`
	Root           string `json:"root,omitempty"`
}

func (h *LoRAAdapterHandler) Create(ctx context.Context, c *vllm.Client, raw json.RawMessage) *resource.ProgressResult {
	var p loraProps
	if err := json.Unmarshal(raw, &p); err != nil || p.LoraName == "" || p.LoraPath == "" {
		return Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "loraName and loraPath are required")
	}
	if err := c.LoadAdapter(ctx, vllm.LoadRequest{
		LoraName: p.LoraName, LoraPath: p.LoraPath,
		BaseModelName: p.BaseModelName, Is3DLoraWeight: p.Is3DLoraWeight,
	}); err != nil {
		return Fail(resource.OperationCreate, mapError(err), err.Error())
	}
	// read-back to capture id/parent/root
	rr := h.Read(ctx, c, p.LoraName)
	if rr.ErrorCode == "" && rr.Properties != "" {
		return Success(resource.OperationCreate, p.LoraName, json.RawMessage(rr.Properties))
	}
	out, _ := json.Marshal(p)
	return Success(resource.OperationCreate, p.LoraName, out)
}

// Read/Update/Delete/List added in later tasks.
```
Add temporary no-op stubs for the other interface methods so it compiles (they're implemented in Tasks 7–11):
```go
func (h *LoRAAdapterHandler) Read(ctx context.Context, c *vllm.Client, nativeID string) *resource.ReadResult {
	return &resource.ReadResult{ResourceType: loraAdapterType, ErrorCode: resource.OperationErrorCodeNotFound}
}
func (h *LoRAAdapterHandler) Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult {
	return Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "not implemented")
}
func (h *LoRAAdapterHandler) Delete(ctx context.Context, c *vllm.Client, nativeID string) *resource.ProgressResult {
	return Fail(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, "not implemented")
}
func (h *LoRAAdapterHandler) List(ctx context.Context, c *vllm.Client) (*resource.ListResult, error) {
	return &resource.ListResult{}, nil
}
```

- [ ] **Step 4: `make install` then run, expect PASS**

Run: `make install && go test -tags=integration -run TestCreate -v .`
Expected: PASS.

- [ ] **Step 5: Commit checkpoint** — "feat: LoRAAdapter Create".

> **GUIDED CHECKPOINT:** present failing test, then passing implementation.

---

## Task 7: LoRAAdapter **Read** + NotFound (TDD)

**Files:** Modify `pkg/handler/loraadapter.go`; extend `vllm_test.go`.

- [ ] **Step 1: Write failing tests**

Append to `vllm_test.go`:
```go
func TestRead(t *testing.T) {
	p, tc := newFakeTarget(t)
	props, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/sql", "baseModelName": "base-model"})
	_, _ = p.Create(context.Background(), &resource.CreateRequest{ResourceType: "VLLM::Inference::LoRAAdapter", Properties: props, TargetConfig: tc})

	rr, err := p.Read(context.Background(), &resource.ReadRequest{
		NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc,
	})
	require.NoError(t, err)
	assert.Empty(t, rr.ErrorCode)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(rr.Properties), &got))
	assert.Equal(t, "sql", got["loraName"])
	assert.Equal(t, "base-model", got["parent"])
}

func TestReadNotFound(t *testing.T) {
	p, tc := newFakeTarget(t)
	rr, err := p.Read(context.Background(), &resource.ReadRequest{
		NativeID: "ghost", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc,
	})
	require.NoError(t, err)
	assert.Equal(t, resource.OperationErrorCodeNotFound, rr.ErrorCode)
}
```

- [ ] **Step 2: Run, expect FAIL** (`TestRead` fails — stub returns NotFound)

Run: `make install && go test -tags=integration -run 'TestRead' -v .`
Expected: `TestRead` FAIL, `TestReadNotFound` PASS (stub already returns NotFound).

- [ ] **Step 3: Implement Read**

Replace the Read stub:
```go
func (h *LoRAAdapterHandler) Read(ctx context.Context, c *vllm.Client, nativeID string) *resource.ReadResult {
	models, err := c.ListModels(ctx)
	if err != nil {
		// transport failure → NetworkFailure/ServiceTimeout, NEVER NotFound
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
```

- [ ] **Step 4: Run, expect PASS**

Run: `make install && go test -tags=integration -run 'TestRead' -v .`
Expected: both PASS.

- [ ] **Step 5: Commit checkpoint** — "feat: LoRAAdapter Read + NotFound".

> **GUIDED CHECKPOINT.**

---

## Task 8: **Read unreachable → NetworkFailure** (TDD — the offline≠deleted guard)

**Files:** extend `vllm_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestReadUnreachable(t *testing.T) {
	p := &Plugin{}
	// Valid-looking but dead endpoint → transport error.
	tc, _ := json.Marshal(map[string]string{"Type": "vllm", "BaseUrl": "http://127.0.0.1:1"})
	rr, err := p.Read(context.Background(), &resource.ReadRequest{
		NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc,
	})
	require.NoError(t, err)
	assert.NotEqual(t, resource.OperationErrorCodeNotFound, rr.ErrorCode, "unreachable must NOT be NotFound (offline != deleted)")
	assert.Contains(t, []resource.OperationErrorCode{
		resource.OperationErrorCodeNetworkFailure, resource.OperationErrorCodeServiceTimeout,
	}, rr.ErrorCode)
}
```

- [ ] **Step 2: Run, expect PASS** (Read already maps transport errors via `mapError`)

Run: `make install && go test -tags=integration -run TestReadUnreachable -v .`
Expected: PASS. (This test *locks in* the behavior; if it fails, Read is mis-mapping transport errors — fix Read, not the test.)

- [ ] **Step 3: Commit checkpoint** — "test: Read unreachable maps to NetworkFailure (offline != deleted)".

> **GUIDED CHECKPOINT:** this is the headline correctness guarantee — present it explicitly.

---

## Task 9: LoRAAdapter **Update** (load_inplace) (TDD)

**Files:** Modify `pkg/handler/loraadapter.go`; extend `vllm_test.go`.

- [ ] **Step 1: Write the failing test**

```go
func TestUpdate(t *testing.T) {
	p, tc := newFakeTarget(t)
	props, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/v1", "baseModelName": "base-model"})
	_, _ = p.Create(context.Background(), &resource.CreateRequest{ResourceType: "VLLM::Inference::LoRAAdapter", Properties: props, TargetConfig: tc})

	prior, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/v1"})
	desired, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/v2", "baseModelName": "base-model"})
	res, err := p.Update(context.Background(), &resource.UpdateRequest{
		NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter",
		PriorProperties: prior, DesiredProperties: desired, TargetConfig: tc,
	})
	require.NoError(t, err)
	assert.Equal(t, resource.OperationStatusSuccess, res.ProgressResult.OperationStatus)

	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc})
	var got map[string]any
	_ = json.Unmarshal([]byte(rr.Properties), &got)
	assert.Equal(t, "/p/v2", got["loraPath"])
}
```

- [ ] **Step 2: Run, expect FAIL** (stub returns InvalidRequest)

Run: `make install && go test -tags=integration -run TestUpdate -v .`
Expected: FAIL.

- [ ] **Step 3: Implement Update**

```go
func (h *LoRAAdapterHandler) Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult {
	var p loraProps
	if err := json.Unmarshal(desired, &p); err != nil || p.LoraPath == "" {
		return Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "loraPath required")
	}
	// Only loraPath is mutable; reload in place.
	if err := c.LoadAdapter(ctx, vllm.LoadRequest{
		LoraName: nativeID, LoraPath: p.LoraPath,
		BaseModelName: p.BaseModelName, Is3DLoraWeight: p.Is3DLoraWeight,
		LoadInplace: true,
	}); err != nil {
		return Fail(resource.OperationUpdate, mapError(err), err.Error())
	}
	rr := h.Read(ctx, c, nativeID)
	if rr.ErrorCode == "" {
		return Success(resource.OperationUpdate, nativeID, json.RawMessage(rr.Properties))
	}
	out, _ := json.Marshal(p)
	return Success(resource.OperationUpdate, nativeID, out)
}
```

- [ ] **Step 4: Run, expect PASS**

Run: `make install && go test -tags=integration -run TestUpdate -v .`
Expected: PASS.

- [ ] **Step 5: Commit checkpoint** — "feat: LoRAAdapter Update (load_inplace)".

> **GUIDED CHECKPOINT.**

---

## Task 10: LoRAAdapter **Delete** + NotFound-as-success (TDD)

**Files:** Modify `pkg/handler/loraadapter.go`; extend `vllm_test.go`.

- [ ] **Step 1: Write failing tests**

```go
func TestDelete(t *testing.T) {
	p, tc := newFakeTarget(t)
	props, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/sql"})
	_, _ = p.Create(context.Background(), &resource.CreateRequest{ResourceType: "VLLM::Inference::LoRAAdapter", Properties: props, TargetConfig: tc})

	res, err := p.Delete(context.Background(), &resource.DeleteRequest{NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc})
	require.NoError(t, err)
	assert.Equal(t, resource.OperationStatusSuccess, res.ProgressResult.OperationStatus)

	rr, _ := p.Read(context.Background(), &resource.ReadRequest{NativeID: "sql", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc})
	assert.Equal(t, resource.OperationErrorCodeNotFound, rr.ErrorCode)
}

func TestDeleteNotFound(t *testing.T) {
	p, tc := newFakeTarget(t)
	res, err := p.Delete(context.Background(), &resource.DeleteRequest{NativeID: "ghost", ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc})
	require.NoError(t, err)
	assert.Equal(t, resource.OperationStatusSuccess, res.ProgressResult.OperationStatus, "deleting an absent adapter is success")
}
```

- [ ] **Step 2: Run, expect FAIL**

Run: `make install && go test -tags=integration -run 'TestDelete' -v .`
Expected: FAIL.

- [ ] **Step 3: Implement Delete**

```go
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
```

- [ ] **Step 4: Run, expect PASS**

Run: `make install && go test -tags=integration -run 'TestDelete' -v .`
Expected: both PASS.

- [ ] **Step 5: Commit checkpoint** — "feat: LoRAAdapter Delete (NotFound = success)".

> **GUIDED CHECKPOINT.**

---

## Task 11: LoRAAdapter **List** + Model discovery (TDD)

**Files:** Modify `pkg/handler/loraadapter.go`; create `pkg/handler/model.go`; extend `vllm_test.go`.

- [ ] **Step 1: Write failing tests**

```go
func TestListAdapters(t *testing.T) {
	p, tc := newFakeTarget(t)
	props, _ := json.Marshal(map[string]any{"loraName": "sql", "loraPath": "/p/sql"})
	_, _ = p.Create(context.Background(), &resource.CreateRequest{ResourceType: "VLLM::Inference::LoRAAdapter", Properties: props, TargetConfig: tc})

	res, err := p.List(context.Background(), &resource.ListRequest{ResourceType: "VLLM::Inference::LoRAAdapter", TargetConfig: tc})
	require.NoError(t, err)
	assert.Equal(t, []string{"sql"}, res.NativeIDs)
}

func TestListModels_DiscoversBase(t *testing.T) {
	p, tc := newFakeTarget(t)
	res, err := p.List(context.Background(), &resource.ListRequest{ResourceType: "VLLM::Inference::Model", TargetConfig: tc})
	require.NoError(t, err)
	assert.Equal(t, []string{"base-model"}, res.NativeIDs)
}
```

- [ ] **Step 2: Run, expect FAIL** (adapter List stub returns empty; Model type unregistered)

Run: `make install && go test -tags=integration -run TestList -v .`
Expected: FAIL.

- [ ] **Step 3: Implement adapter List**

Replace the LoRAAdapter `List` stub:
```go
func (h *LoRAAdapterHandler) List(ctx context.Context, c *vllm.Client) (*resource.ListResult, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, m := range models {
		if m.Parent != nil { // adapters have a parent
			ids = append(ids, m.ID)
		}
	}
	return &resource.ListResult{NativeIDs: ids}, nil
}
```

- [ ] **Step 4: Implement the Model handler**

`pkg/handler/model.go`:
```go
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
		if m.ID == nativeID && m.Parent == nil { // base models have no parent
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

// Discovery/read-only: mutating ops are unsupported.
func (h *ModelHandler) Create(ctx context.Context, c *vllm.Client, props json.RawMessage) *resource.ProgressResult {
	return Fail(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
func (h *ModelHandler) Update(ctx context.Context, c *vllm.Client, nativeID string, prior, desired json.RawMessage) *resource.ProgressResult {
	return Fail(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
func (h *ModelHandler) Delete(ctx context.Context, c *vllm.Client, nativeID string) *resource.ProgressResult {
	return Fail(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, "VLLM::Inference::Model is discovery-only")
}
```

- [ ] **Step 5: Run, expect PASS**

Run: `make install && go test -tags=integration -run TestList -v .`
Expected: both PASS.

- [ ] **Step 6: Full integration suite green**

Run: `make install && go test -tags=integration -v .`
Expected: all PASS.

- [ ] **Step 7: Commit checkpoint** — "feat: List adapters + Model discovery".

> **GUIDED CHECKPOINT:** present discovery implementation.

---

## Task 12: Conformance wiring (fake-backed)

**Files:**
- Verify/edit: `conformance_test.go` (scaffolded `RunCRUDTests`/`RunDiscoveryTests` — keep as-is).
- Create: `testdata/loraadapter.pkl`, `testdata/loraadapter-update.pkl`, `testdata/loraadapter-replace.pkl`
- Modify: `Makefile` — add a target that boots the fake vLLM, exports `VLLM_URL`, runs conformance, tears down.

- [ ] **Step 1: Write the CRUD fixture**

Model after grafana's `testdata/datasource.pkl` (keep its `amends`/`import` preamble; swap resource). `testdata/loraadapter.pkl`:
```pkl
amends "@formae/forma.pkl"
import "@formae/formae.pkl"
import "@vllm/loraadapter.pkl"

local testRunID = read("env:FORMAE_TEST_RUN_ID")

forma {
  new formae.Stack { label = "plugin-sdk-test-stack" description = "vLLM conformance" }

  new formae.Target {
    label = "vllm-target"
    namespace = "VLLM"
    config = new Mapping {
      ["Type"] = "vllm"
      ["BaseUrl"] = read("env:VLLM_URL")
    }
  }

  new loraadapter.LoRAAdapter {
    label = "test-adapter"
    loraName = "conf-\(testRunID)"
    loraPath = "/models/adapters/conf-\(testRunID)"
    baseModelName = "Qwen/Qwen2.5-0.5B-Instruct"
  }
}
```
`testdata/loraadapter-update.pkl`: identical but `loraPath = "/models/adapters/conf-\(testRunID)-v2"` (mutable → Update).
`testdata/loraadapter-replace.pkl`: identical but `loraName = "conf-\(testRunID)-r"` (CreateOnly → replacement).

- [ ] **Step 2: Add Makefile conformance-with-fake target**

Append to `Makefile`:
```makefile
export VLLM_URL ?= http://127.0.0.1:8000

fake-vllm-up:
	@FAKE_VLLM_ADDR=:8000 FAKE_VLLM_BASE_MODEL=Qwen/Qwen2.5-0.5B-Instruct \
		$(GO) run ./cmd/fake-vllm & echo $$! > .fake-vllm.pid
	@sleep 1

fake-vllm-down:
	@if [ -f .fake-vllm.pid ]; then kill $$(cat .fake-vllm.pid) 2>/dev/null || true; rm -f .fake-vllm.pid; fi

conformance-test-fake: fake-vllm-up
	@$(MAKE) conformance-test; STATUS=$$?; $(MAKE) fake-vllm-down; exit $$STATUS
```
(The fake serves a static base model so discovery has something to find; CRUD loads/unloads adapters in memory.)

- [ ] **Step 3: Run conformance against the fake**

Run: `make install && make conformance-test-fake`
Expected: CRUD lifecycle (create→read→update→replace→delete→OOB-delete→sync→removed) PASS; discovery finds the base model. Fix the fake or handlers until green. If the harness expects behaviors the fake doesn't model (e.g. specific response fields), extend `internal/fakevllm/server.go` to match documented vLLM responses and re-run.

- [ ] **Step 4: Commit checkpoint** — "test: conformance suite against fake vLLM".

> **GUIDED CHECKPOINT:** conformance green is a gate. Present results.

---

## Task 13: Local GPU enablement (NVIDIA Container Toolkit)

**Files:** none in repo (host setup). Document in README later.

- [ ] **Step 1: Confirm the GPU is visible**

Run: `/usr/lib/wsl/lib/nvidia-smi --query-gpu=name,memory.total --format=csv`
Expected: `NVIDIA GeForce RTX 5090, 32607 MiB`.

- [ ] **Step 2: Install the toolkit**

Run (Ubuntu/WSL2):
```bash
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list
sudo apt-get update && sudo apt-get install -y nvidia-container-toolkit
sudo nvidia-ctk runtime configure --runtime=docker
sudo service docker restart 2>/dev/null || sudo systemctl restart docker
```
(This step needs the user's sudo — if a password prompt blocks, ask the user to run it via `! <cmd>`.)

- [ ] **Step 3: Verify Docker GPU access**

Run: `docker run --rm --gpus all nvidia/cuda:12.6.2-base-ubuntu24.04 nvidia-smi`
Expected: the 5090 listed inside the container.

- [ ] **Step 4: Commit checkpoint** — none (host setup). Record the commands in README in Task 16.

---

## Task 14: Local 5090 real-model e2e

**Files:** Create `examples/local/docker-compose.yml`, `examples/local/forma.pkl`, `scripts/e2e-local.sh`.

- [ ] **Step 1: docker-compose for real vLLM**

`examples/local/docker-compose.yml`:
```yaml
services:
  vllm:
    image: vllm/vllm-openai:latest   # must include Blackwell/sm_120 kernels; pin a tag that does
    command: >
      --model Qwen/Qwen2.5-0.5B-Instruct
      --enable-lora --max-lora-rank 32
    environment:
      - VLLM_ALLOW_RUNTIME_LORA_UPDATING=True
      - HF_HOME=/models
    volumes:
      - ./models:/models
    ports:
      - "8000:8000"
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

- [ ] **Step 2: Validate the model/adapter pair (spec validation gate)**

Run: `docker compose -f examples/local/docker-compose.yml up -d` then once healthy:
```bash
curl -s localhost:8000/v1/models
curl -s -X POST localhost:8000/v1/load_lora_adapter -H 'Content-Type: application/json' \
  -d '{"lora_name":"demo","lora_path":"<downloaded adapter path under ./models>"}'
curl -s localhost:8000/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"model":"demo","messages":[{"role":"user","content":"hi"}]}'
```
Expected: load returns 200; chat against `model=demo` returns 200 with adapter-influenced output. **Lock the chosen `Qwen2.5-0.5B-Instruct` + adapter pair** here (fallback `unsloth/Llama-3.2-1B-Instruct` if dimension mismatch). Update the spec's "Model / adapter selection" with the final pair.

- [ ] **Step 3: forma + e2e script**

`examples/local/forma.pkl`:
```pkl
amends "@formae/forma.pkl"
import "@formae/formae.pkl"
import "@vllm/loraadapter.pkl"

forma {
  new formae.Stack { label = "vllm-local" }
  new formae.Target {
    label = "local-vllm"
    namespace = "VLLM"
    config = new Mapping { ["Type"] = "vllm" ["BaseUrl"] = "http://localhost:8000" }
  }
  new loraadapter.LoRAAdapter {
    label = "demo-adapter"
    loraName = "demo"
    loraPath = "/models/<adapter>"
    baseModelName = "Qwen/Qwen2.5-0.5B-Instruct"
  }
}
```
`scripts/e2e-local.sh` runs: `make install` → `formae apply --mode reconcile --simulate` → `formae apply --mode reconcile` → `formae inventory` (adapter managed) → query base vs `model=demo` → `curl` OOB-unload → `formae` sync → show removed → `formae apply` restores → `formae destroy`.

- [ ] **Step 4: Run the local e2e**

Run: `bash scripts/e2e-local.sh`
Expected: full lifecycle passes; OOB-delete detected and restored via re-apply.

- [ ] **Step 5: Offline-node demo (≥2 fake/real nodes)**

Bring up a second vLLM (port 8001) or a fake; declare an adapter on each (two targets). Stop one; `formae apply`; confirm the reachable node converges and the stopped node reports `unreachable` (not tombstoned). Restart it; re-apply; confirm convergence. Document in README.

- [ ] **Step 6: Commit checkpoint** — "feat: local 5090 e2e example + scripts".

> **GUIDED CHECKPOINT.**

---

## Task 15: AWS dogfood e2e (billable — requires go-ahead)

**Files:** Create `examples/aws/box.pkl` (EC2 via AWS plugin), `examples/aws/adapter.pkl`, `scripts/e2e-aws.sh`.

- [ ] **Step 1: STOP — get explicit user go-ahead**

This launches a billable GPU instance (`g4dn.xlarge` ≈ $0.53/hr). Ask the user to confirm before running anything in this task.

- [ ] **Step 2: forma to bring up the box (dogfood AWS plugin)**

`examples/aws/box.pkl`: an EC2 GPU instance (`g4dn.xlarge`) with a Deep Learning AMI and user-data that runs the vLLM container (`--enable-lora`, `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True`, port 8000 open to the dev IP only). Use the AWS plugin's EC2 resource types; reference `/home/jeroen/dev/pel/formae-plugin-aws/examples/` for exact shapes.

- [ ] **Step 3: Apply, capture endpoint**

Run: `formae apply --mode reconcile examples/aws/box.pkl` → wait healthy → capture public IP.

- [ ] **Step 4: Manage the adapter against the EC2 endpoint**

`examples/aws/adapter.pkl` target `BaseUrl = http://<ec2-ip>:8000`; declare a `LoRAAdapter`. Run apply/inventory/drift/destroy as in Task 14.

- [ ] **Step 5: Tear down**

Run: `formae destroy` for both stacks. Verify the instance is terminated (`aws ec2 describe-instances`).

- [ ] **Step 6: Commit checkpoint** — "feat: AWS dogfood e2e example + scripts".

> **GUIDED CHECKPOINT.**

---

## Task 16: Kubernetes example

**Files:** Create `examples/kubernetes/vllm-deployment.yaml`, `examples/kubernetes/forma.pkl`, `examples/kubernetes/README.md`.

- [ ] **Step 1: vLLM Deployment + Service**

`examples/kubernetes/vllm-deployment.yaml`:
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vllm
  labels: { app: vllm }
spec:
  replicas: 1
  selector: { matchLabels: { app: vllm } }
  template:
    metadata: { labels: { app: vllm } }
    spec:
      containers:
        - name: vllm
          image: vllm/vllm-openai:latest
          args: ["--model", "Qwen/Qwen2.5-0.5B-Instruct", "--enable-lora", "--max-lora-rank", "32"]
          env:
            - { name: VLLM_ALLOW_RUNTIME_LORA_UPDATING, value: "True" }
          ports: [{ containerPort: 8000 }]
          resources:
            limits: { nvidia.com/gpu: 1 }
          volumeMounts:
            - { name: adapters, mountPath: /models/adapters }
      volumes:
        - name: adapters
          persistentVolumeClaim: { claimName: vllm-adapters }
---
apiVersion: v1
kind: Service
metadata: { name: vllm }
spec:
  selector: { app: vllm }
  ports: [{ port: 8000, targetPort: 8000 }]
```

- [ ] **Step 2: forma targeting the Service**

`examples/kubernetes/forma.pkl`: target `BaseUrl = http://vllm.<namespace>.svc.cluster.local:8000` (in-cluster) or the port-forwarded `http://localhost:8000`; declare a `LoRAAdapter` with `loraPath` under the mounted PVC.

- [ ] **Step 3: Example README**

`examples/kubernetes/README.md`: explain that vLLM is provisioned by k8s (CoreWeave/GKE/EKS/edge-k3s) and formae manages adapters against the Service endpoint; note the adapter PVC is the distribution layer; note the Axis-B replica caveat (single replica here; >1 needs a fleet controller — see spec "Scaling & distribution").

- [ ] **Step 4: Commit checkpoint** — "docs: kubernetes example".

---

## Task 17: README + Definition-of-Done verification

**Files:** Create `README.md`.

- [ ] **Step 1: Write README**

Sections: overview; supported resource types (`LoRAAdapter` full CRUD; `Model` discovery/read-only) with the field tables from the spec; target config (`BaseUrl`) + credentials (`VLLM_API_KEY`); server prerequisites (`--enable-lora`, `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True`); examples pointers (local/aws/kubernetes); offline/`unreachable` behavior + the "restore via re-apply, not auto-heal" note with a PLA-5 pointer; scaling/distribution note.

- [ ] **Step 2: Verify the DoD from the spec**

Run and confirm each:
```bash
make install && go test -tags=integration -v .      # all green (incl. TestReadUnreachable)
make install && make conformance-test-fake           # CRUD + discovery green
bash scripts/e2e-local.sh                             # local real-model lifecycle + offline demo
```
Check the spec's "Definition of done" checklist item-by-item; tick each.

- [ ] **Step 3: Final commit checkpoint** — "docs: README + DoD".

> **GUIDED CHECKPOINT:** present the completed plugin and DoD evidence.

---

## Notes carried from the spec / investigation

- **Error mapping is the crux:** transport failure → `NetworkFailure`/`ServiceTimeout` as `ReadResult.ErrorCode` (nil Go error); authoritative absence (200 list without entry) → `NotFound`. Never return a Go error for transport failure (→ `UnforeseenError`, terminal). Locked by `TestReadUnreachable`.
- **Restoration is via `formae apply`, not auto-heal** under current core behavior; hands-off self-heal depends on PLA-5.
- **Blackwell (sm_120):** pin a `vllm/vllm-openai` image tag known to ship sm_120 kernels; verify in Task 14 Step 2 before relying on it.
- **api_key** lives in `VLLM_API_KEY` (env), not the forma file (secret hygiene; mirrors grafana). `BaseUrl` is the only target-config field.
