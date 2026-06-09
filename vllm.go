// © 2026 Platform Engineering Labs
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/config"
	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/handler"
	"github.com/platform-engineering-labs/formae-plugin-vllm/pkg/vllm"
	"github.com/platform-engineering-labs/formae/pkg/model"
	"github.com/platform-engineering-labs/formae/pkg/plugin"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

// Plugin implements the Formae ResourcePlugin interface for vLLM.
type Plugin struct {
	mu      sync.Mutex
	clients map[string]*vllm.Client
}

var _ plugin.ResourcePlugin = &Plugin{}

func (p *Plugin) RateLimit() model.RateLimitConfig {
	return model.RateLimitConfig{
		Scope:                            model.RateLimitScopeNamespace,
		MaxRequestsPerSecondForNamespace: 10,
	}
}

func (p *Plugin) DiscoveryFilters() []model.MatchFilter { return nil }

func (p *Plugin) LabelConfig() model.LabelConfig {
	return model.LabelConfig{
		DefaultQuery:      "$.id",
		ResourceOverrides: map[string]string{},
	}
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

func (p *Plugin) Status(ctx context.Context, req *resource.StatusRequest) (*resource.StatusResult, error) {
	return &resource.StatusResult{ProgressResult: handler.Fail(resource.OperationCheckStatus, resource.OperationErrorCodeInvalidRequest, "status not supported (synchronous plugin)")}, nil
}

func (p *Plugin) List(ctx context.Context, req *resource.ListRequest) (*resource.ListResult, error) {
	h, ok := handler.For(req.ResourceType)
	if !ok {
		return &resource.ListResult{NativeIDs: []string{}}, nil
	}
	c, err := p.client(req.TargetConfig)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, nil
	}
	return h.List(ctx, c)
}
