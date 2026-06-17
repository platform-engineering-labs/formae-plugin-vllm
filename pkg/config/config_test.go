package config

import "testing"

func TestParseTargetConfig_BaseURL(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","BaseUrl":"http://node:8000"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "http://node:8000" {
		t.Fatalf("BaseURL = %q, want http://node:8000", cfg.BaseURL)
	}
}

func TestParseTargetConfig_HostPortBuildsURL(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","Host":"1.2.3.4"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "http://1.2.3.4:8000" {
		t.Fatalf("BaseURL = %q, want http://1.2.3.4:8000 (default scheme+port)", cfg.BaseURL)
	}
}

func TestParseTargetConfig_HostPortSchemeExplicit(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","Host":"node","Port":9000,"Scheme":"https"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://node:9000" {
		t.Fatalf("BaseURL = %q, want https://node:9000", cfg.BaseURL)
	}
}

func TestParseTargetConfig_SchemeExplicitDefaultPort(t *testing.T) {
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","Host":"node","Scheme":"https"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "https://node:8000" {
		t.Fatalf("BaseURL = %q, want https://node:8000 (explicit scheme, default port)", cfg.BaseURL)
	}
}

func TestParseTargetConfig_BaseURLWins(t *testing.T) {
	// If both are present, BaseUrl is used verbatim.
	cfg, err := ParseTargetConfig([]byte(`{"Type":"vllm","BaseUrl":"http://explicit:1","Host":"ignored"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BaseURL != "http://explicit:1" {
		t.Fatalf("BaseURL = %q, want http://explicit:1", cfg.BaseURL)
	}
}

func TestParseTargetConfig_NeitherIsError(t *testing.T) {
	if _, err := ParseTargetConfig([]byte(`{"Type":"vllm"}`)); err == nil {
		t.Fatal("expected error when neither BaseUrl nor Host is set")
	}
}
