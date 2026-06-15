# Local infra-graph example (no GPU, no cloud)

Demonstrates the same **AWS → vLLM → ChatUI** graph as the AWS fleet demo, but
entirely locally and GPU-free, so the cross-plugin **resolvable wiring** can be
validated without provisioning anything billable.

```
fake vLLM ──/v1/models──► LoRAAdapter "chat" ──.res.id (MODEL)──► chat-UI container
                                                                  (compose variables)
```

Every edge is a formae resolvable, resolved late at apply time:

- The vLLM **target** builds its `BaseURL` from `Host`/`Port` (→ `http://localhost:8077`).
- The `LoRAAdapter`'s **`res.id`** is wired into the chat-UI's `MODEL` via the
  compose **`variables`** field, which formae injects as the compose project's
  env so `${MODEL}` interpolates in the `composeFile`.

## Prerequisites

- Docker (Compose v2), with `host-gateway` support (Docker 20.10+).
- A formae agent running against the store that holds the **locally-built**
  vLLM + compose plugins:

  ```bash
  FORMAE_PEL_ROOT=$HOME/.pel formae agent start
  ```

- The chat-UI image built locally:

  ```bash
  docker build -t formae-chat-ui:demo examples/aws/chat-ui
  ```

- A **fake vLLM** running on the host (stands in for a GPU vLLM server; supports
  the adapter load/unload + `/v1/models` endpoints the plugin uses, plus a canned
  `/v1/chat/completions` so the chat UI answers):

  ```bash
  FAKE_VLLM_ADDR=:8077 go run ./cmd/fake-vllm
  ```

## Run

```bash
# Resolve the local plugin schema deps once.
cd examples/local-graph && pkl project resolve && cd -

# Dry run, then apply.
formae apply --mode reconcile --simulate examples/local-graph/forma.pkl
formae apply --mode reconcile          examples/local-graph/forma.pkl
```

Then open <http://localhost:8088>. The page header shows the resolved wiring
(`model chat @ http://host.docker.internal:8077 — wired by formae resolvables`),
and a prompt gets a reply proxied through the chat-UI to the fake vLLM.

## Teardown

```bash
formae destroy --yes examples/local-graph/forma.pkl
```

> Note: this example imports the vLLM and compose plugin **schemas from the
> working trees** (`../../schema/pkl/PklProject` and the sibling
> `formae-plugin-compose` repo), because the compose `variables` field and the
> `LoRAAdapterResolvable` are not yet in a published package. Once compose ships
> a release with `variables`, switch the compose dependency to the published
> package URI.
