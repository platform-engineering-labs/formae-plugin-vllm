# Local chat-UI demo (no GPU, no cloud)

Run the same vLLM + chat-UI demo as the AWS fleet example — a web chat UI talking
to a model — **entirely on your machine, with no GPU and no cloud**. It uses a
stand-in (fake) vLLM so you can see the whole thing work in seconds before
committing to cloud GPUs.

The point: formae wires the pieces together for you. Nothing is copied by hand —
the chat-UI learns which model to call and where to reach it from the other
resources, resolved at apply time:

```
fake vLLM ──/v1/models──► LoRAAdapter "chat" ──.res.id (MODEL)──► chat-UI container
                                                                  (compose variables)
```

- The vLLM **target** builds its endpoint from `host`/`port` (→ `http://localhost:8077`).
- The `LoRAAdapter`'s read-back **`res.id`** is fed into the chat-UI's `MODEL` via
  the compose **`variables`** field, which formae injects as the container's env so
  `${MODEL}` interpolates in the compose file.

## Prerequisites

- Docker (Compose v2), with `host-gateway` support (Docker 20.10+).
- A running formae agent with the **vLLM** and **compose** plugins installed.
- The chat-UI image built locally:

  ```bash
  docker build -t formae-chat-ui:demo examples/aws/chat-ui
  ```

- A **fake vLLM** running on the host (stands in for a GPU vLLM server — it serves
  the adapter load/unload + `/v1/models` endpoints the plugin uses, plus a canned
  `/v1/chat/completions` so the chat UI answers):

  ```bash
  FAKE_VLLM_ADDR=:8077 go run ./cmd/fake-vllm
  ```

## Run

```bash
cd examples/local-chat-ui && pkl project resolve && cd -

formae apply --mode reconcile --simulate examples/local-chat-ui/forma.pkl   # dry run
formae apply --mode reconcile          examples/local-chat-ui/forma.pkl     # for real
```

Then open <http://localhost:8088>. The page shows what formae resolved
(`model chat @ http://host.docker.internal:8077`), and a prompt gets a reply
proxied through the chat-UI to the fake vLLM.

## Teardown

```bash
formae destroy --yes examples/local-chat-ui/forma.pkl
```
