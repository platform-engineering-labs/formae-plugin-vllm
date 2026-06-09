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
