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
	defer func() { _ = resp.Body.Close() }()
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
