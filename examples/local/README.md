# Local Docker (GPU) example

Run a real vLLM server locally on an NVIDIA GPU and let formae manage a LoRA
adapter on it. Serves `Qwen/Qwen2.5-0.5B-Instruct` (Apache-2.0, ungated) on
`http://localhost:8000`.

## Prerequisites

- Docker (with Compose v2: `docker compose`).
- An NVIDIA GPU and recent driver.
- The **NVIDIA Container Toolkit**, so Docker can pass the GPU into the
  container:

  ```bash
  curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey \
    | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
  curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list \
    | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' \
    | sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

  sudo apt-get update
  sudo apt-get install -y nvidia-container-toolkit
  sudo nvidia-ctk runtime configure --runtime=docker
  sudo systemctl restart docker
  ```

  > **Blackwell note:** on a 5090 / other Blackwell (sm_120) card, make sure the
  > `vllm/vllm-openai` image you pull ships sm_120 kernels (use a recent image and
  > verify its bundled torch supports sm_120 before relying on it).

## Steps

1. **Start vLLM:**

   ```bash
   docker compose up -d
   # wait until it is serving:
   curl -s http://localhost:8000/v1/models | jq
   ```

2. **Stage a LoRA adapter.** The forma references `/models/demo-adapter`, and
   `./models` is mounted at `/models` in the container. Use a real, public,
   dimension-matched Qwen2.5-0.5B LoRA (the same pair the conformance suite
   validates):

   ```bash
   mkdir -p models/demo-adapter
   base=https://huggingface.co/taronklm/Qwen2.5-0.5B-Instruct-lora-chatbot/resolve/main
   curl -fsSL "$base/adapter_config.json"      -o models/demo-adapter/adapter_config.json
   curl -fsSL "$base/adapter_model.safetensors" -o models/demo-adapter/adapter_model.safetensors
   ```

   In an air-gapped environment, copy/rsync the adapter onto the node first —
   formae only *activates* what is already on disk; it never moves the bytes.

3. **Install the plugin** (from the repo root):

   ```bash
   make install
   ```

4. **Resolve pkl dependencies** for this example:

   ```bash
   cd examples/local && pkl project resolve
   ```

5. **Apply** — simulate first, then for real:

   ```bash
   formae apply --mode reconcile --simulate examples/local/forma.pkl
   formae apply --mode reconcile           examples/local/forma.pkl
   ```

6. **Verify** the adapter is loaded and addressable as its own model id:

   ```bash
   formae inventory

   # base model
   curl -s http://localhost:8000/v1/chat/completions \
     -H 'Content-Type: application/json' \
     -d '{"model":"Qwen/Qwen2.5-0.5B-Instruct","messages":[{"role":"user","content":"hi"}]}'

   # the adapter, addressed by its loraName ("demo")
   curl -s http://localhost:8000/v1/chat/completions \
     -H 'Content-Type: application/json' \
     -d '{"model":"demo","messages":[{"role":"user","content":"hi"}]}'
   ```

7. **Destroy** when done:

   ```bash
   formae destroy examples/local/forma.pkl
   docker compose down
   ```

## Offline behavior

If the node becomes unreachable (container stopped, port blocked), formae reports
it as **unreachable** (a recoverable `NetworkFailure`) and retries — it does
**not** treat the adapter as deleted. Offline ≠ deleted.

If the adapter is unloaded out-of-band on a *reachable* node (e.g. someone
`curl`s `/v1/unload_lora_adapter`), sync sees an authoritative absence and
tombstones it from inventory. Restore it by re-running `formae apply` on this
forma — re-apply is idempotent (loads if missing, no-ops if present).
