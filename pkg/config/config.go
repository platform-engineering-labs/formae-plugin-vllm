package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// TargetConfig is the per-node connection config carried in the forma Target.
// A target supplies EITHER an explicit BaseUrl, OR a Host (+ optional Port/Scheme)
// from which BaseURL is built. Host is what lets a target resolve its endpoint
// from another resource (e.g. an AWS instance's PublicIp) via a formae resolvable.
type TargetConfig struct {
	Type    string `json:"Type"`
	BaseURL string `json:"BaseUrl"`
	Host    string `json:"Host"`
	Port    int    `json:"Port"`
	Scheme  string `json:"Scheme"`
}

// ParseTargetConfig decodes the JSON target config. If BaseUrl is set it is used
// verbatim; otherwise BaseURL is built from Host (+ Port default 8000, Scheme
// default http). It is an error if neither BaseUrl nor Host is present.
func ParseTargetConfig(data json.RawMessage) (*TargetConfig, error) {
	var cfg TargetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid target config: %w", err)
	}
	if cfg.BaseURL != "" {
		return &cfg, nil
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("target config must set either 'BaseUrl' or 'Host'")
	}
	scheme := cfg.Scheme
	if scheme == "" {
		scheme = "http"
	}
	port := cfg.Port
	if port == 0 { // 0 = absent in JSON; default to vLLM's standard port (not scheme-derived)
		port = 8000
	}
	cfg.BaseURL = fmt.Sprintf("%s://%s:%d", scheme, cfg.Host, port)
	return &cfg, nil
}

// APIKey returns the optional bearer token from the environment. Empty = no auth.
func APIKey() string {
	return os.Getenv("VLLM_API_KEY")
}
