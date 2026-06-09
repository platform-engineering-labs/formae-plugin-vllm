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

	body := get(t, c, srv.URL+"/v1/models")
	if strings.Contains(body, "sql") {
		t.Fatal("adapter present before load")
	}
	post(t, c, srv.URL+"/v1/load_lora_adapter", `{"lora_name":"sql","lora_path":"/p/sql"}`, 200)
	body = get(t, c, srv.URL+"/v1/models")
	if !strings.Contains(body, `"id":"sql"`) || !strings.Contains(body, `"parent":"base-model"`) {
		t.Fatalf("adapter not listed: %s", body)
	}
	post(t, c, srv.URL+"/v1/unload_lora_adapter", `{"lora_name":"sql"}`, 200)
	body = get(t, c, srv.URL+"/v1/models")
	if strings.Contains(body, `"id":"sql"`) {
		t.Fatalf("adapter still listed after unload: %s", body)
	}
	post(t, c, srv.URL+"/v1/unload_lora_adapter", `{"lora_name":"sql"}`, 404)
}

func get(t *testing.T, c *http.Client, url string) string {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b := make([]byte, 4096)
	n, _ := resp.Body.Read(b)
	return string(b[:n])
}

func post(t *testing.T, c *http.Client, url, body string, want int) {
	t.Helper()
	resp, err := c.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != want {
		t.Fatalf("POST %s -> %d, want %d", url, resp.StatusCode, want)
	}
}
