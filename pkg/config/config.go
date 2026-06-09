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
func APIKey() string {
	return os.Getenv("VLLM_API_KEY")
}
