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
	case "/v1/chat/completions":
		s.chat(w, r)
	default:
		http.NotFound(w, r)
	}
}

// chat is a canned OpenAI-compatible chat-completions endpoint. The real vLLM
// runs the model; the fake just echoes, so a local demo (e.g. the chat-UI proxy)
// gets a well-formed reply that names the model it was routed to. Not used by the
// plugin's CRUD — it exists so the local infra-graph example answers a prompt.
func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	var prompt string
	if n := len(req.Messages); n > 0 {
		prompt = req.Messages[n-1].Content
	}
	reply := "[fake vLLM] model '" + req.Model + "' received: " + prompt
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	type choice struct {
		Index   int     `json:"index"`
		Message message `json:"message"`
	}
	out := struct {
		Object  string   `json:"object"`
		Model   string   `json:"model"`
		Choices []choice `json:"choices"`
	}{
		Object:  "chat.completion",
		Model:   req.Model,
		Choices: []choice{{Index: 0, Message: message{Role: "assistant", Content: reply}}},
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
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
		// Mirror real vLLM's message so error classification is exercised faithfully.
		http.Error(w, "The lora adapter '"+req.LoraName+"' has already been loaded. If you want to load the adapter in place, set 'load_inplace' to True.", http.StatusBadRequest)
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
