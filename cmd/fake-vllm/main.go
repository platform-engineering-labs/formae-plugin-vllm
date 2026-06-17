// Command fake-vllm runs the in-memory fake vLLM OpenAI server as a standalone
// process, so local examples/demos can manage LoRA adapters (and get canned chat
// replies) without a GPU. It is NOT a substitute for real-vLLM conformance.
//
// Usage:
//
//	fake-vllm                       # serve on :8000, base model "base-model"
//	FAKE_VLLM_ADDR=:9001 fake-vllm  # custom listen address
//	FAKE_VLLM_BASE_MODEL=Qwen/... fake-vllm
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/platform-engineering-labs/formae-plugin-vllm/internal/fakevllm"
)

func main() {
	addr := os.Getenv("FAKE_VLLM_ADDR")
	if addr == "" {
		addr = ":8000"
	}
	base := os.Getenv("FAKE_VLLM_BASE_MODEL")
	if base == "" {
		base = "base-model"
	}
	log.Printf("fake-vllm serving base model %q on %s", base, addr)
	if err := http.ListenAndServe(addr, fakevllm.New(base)); err != nil {
		log.Fatalf("fake-vllm: %v", err)
	}
}
