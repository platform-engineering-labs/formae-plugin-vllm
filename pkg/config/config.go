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

	// APIKey is the bearer token when declared in the target config, where it
	// may originate from a formae-managed secret that the agent resolves live
	// before every call. A pointer so a declared but empty key is
	// distinguishable from an absent one.
	APIKey *string `json:"ApiKey,omitempty"`
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

// APIKey returns the bearer token to authenticate with, preferring the target
// config over VLLM_API_KEY. An empty result means no auth, which is a supported
// setup: a vLLM server started without --api-key accepts unauthenticated calls.
//
// A key declared in the target config is used as given. Falling back from an
// empty one would authenticate as whoever the environment names, and sending no
// key at all would silently talk to the server unauthenticated, so both are
// reported instead.
func APIKey(cfg *TargetConfig) (string, error) {
	if cfg != nil && cfg.APIKey != nil {
		if *cfg.APIKey == "" {
			return "", fmt.Errorf("vllm api key in target config is empty; omit ApiKey to use VLLM_API_KEY or to call an unauthenticated server")
		}
		return *cfg.APIKey, nil
	}
	return os.Getenv("VLLM_API_KEY"), nil
}
