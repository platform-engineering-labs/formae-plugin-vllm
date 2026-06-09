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
	_, err := New("http://127.0.0.1:1/", "").ListModels(context.Background())
	if err == nil {
		t.Fatal("expected transport error")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport error should NOT be an APIError, got %v", err)
	}
}
