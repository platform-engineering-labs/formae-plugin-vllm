# formae vLLM plugin

A [formae](https://docs.formae.io) resource plugin for declaratively managing
**LoRA adapters** on a running [vLLM](https://docs.vllm.ai) server and
**discovering** the base models it serves. It is built for edge / sovereign
inference: formae continuously reconciles what a vLLM node serves and detects
out-of-band drift, which is the hard part of running disconnected, customer-owned
inference fleets (the Sector 88 use case — sovereign LLM inference on air-gapped,
pre-existing GPU hardware).

The plugin assumes vLLM is already running; it does not provision hosts or GPUs.
Its target is an already-running, OpenAI-compatible vLLM endpoint, which it
manages over HTTP.

## Supported resource types

### `VLLM::Inference::LoRAAdapter` (full CRUD)

A dynamically-loaded LoRA adapter on a running vLLM server.

| Field            | Type    | Kind                  | Notes                                                          |
|------------------|---------|-----------------------|----------------------------------------------------------------|
| `loraName`       | String  | required, create-only | Adapter name; also the model id used at inference time         |
| `loraPath`       | String  | required, **mutable** | Filesystem path to the adapter on the vLLM node                |
| `baseModelName`  | String  | optional, create-only | Base model this adapter attaches to                            |
| `is3dLoraWeight` | Boolean | optional, create-only | MoE 3D weight layout flag (default `false`)                    |
| `id`             | String  | read-only             | Read-back model id (provider-populated)                        |
| `parent`         | String  | read-only             | Read-back base model id (provider-populated)                   |
| `root`           | String  | read-only             | Read-back artifact location (provider-populated)               |

Changing `loraName` or `baseModelName` (create-only) triggers a replacement;
`loraPath` is updated in place (reload).

### `VLLM::Inference::Model` (discovery / read-only)

A base model served by a vLLM node. Base models are set at vLLM **startup**, not
via the API, so formae can only observe/discover them — never create, update, or
delete.

| Field  | Type   | Kind      | Notes                                              |
|--------|--------|-----------|----------------------------------------------------|
| `id`   | String | read-only | Served base model id (provider-populated)          |
| `root` | String | read-only | Artifact location (provider-populated)             |

## Target configuration

Configure a target with the vLLM node's OpenAI base URL:

```pkl
new formae.Target {
  label = "local-vllm"
  namespace = "VLLM"
  config = new Mapping {
    ["Type"] = "vllm"
    ["BaseUrl"] = "http://localhost:8000"
  }
}
```

| Config key | Required | Notes                                                       |
|------------|----------|-------------------------------------------------------------|
| `Type`     | yes      | Must be `"vllm"`                                             |
| `BaseUrl`  | yes      | vLLM OpenAI base URL, e.g. `http://<node>:8000`             |

An optional bearer token is read from the **`VLLM_API_KEY`** environment variable
(sent as `Authorization: Bearer <key>`); it is intentionally **not** part of the
forma. Leave it unset for an unauthenticated server.

**vLLM server prerequisites.** The server must be started with `--enable-lora`
and the environment variable `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True` so that
`/v1/load_lora_adapter` and `/v1/unload_lora_adapter` are accepted.

## How LoRA adapters work

Once an adapter is loaded, vLLM exposes it as its **own model id**: consumers call
`/v1/chat/completions` with `"model": "<loraName>"` and vLLM routes through the
base model plus the adapter weights. The base model remains addressable by its
own id.

## Examples

- [`examples/local/`](examples/local/) — run vLLM locally on a GPU via
  docker-compose and manage an adapter on it.
- [`examples/kubernetes/`](examples/kubernetes/) — vLLM provisioned by Kubernetes
  (Deployment + PVC + Service); formae manages adapters against the Service.
- [`examples/aws/`](examples/aws/) — dogfood the formae AWS plugin to bring up a
  GPU box, then manage the adapter on it (billable; apply manually).

## Building & testing

```bash
make build                       # build the plugin binary
make install                     # build + install locally (binary + schema + manifest)

go test ./...                    # unit tests
go test -tags=integration .      # integration tests (run against an in-process
                                 #   fake vLLM server — no GPU required)
make conformance-test            # conformance tests against a REAL vLLM: boots a
                                 #   CPU-only vLLM container (Docker, no GPU),
                                 #   runs the CRUD + discovery lifecycle, tears down
```

Conformance always runs against real vLLM — the in-process fake backs the
integration tests only. Idempotency, provider-populated `id`/`parent`/`root` and
path normalization can only be proven against a real server. To point conformance
at an already-running vLLM (e.g. a GPU box) instead of the managed container:

```bash
make conformance-test VLLM_EXTERNAL=1 VLLM_URL=http://<host>:8000
```

## Offline behavior

Edge nodes are intermittently connected, so this is first-class behavior. An
unreachable node (connection refused / timeout / DNS / TLS failure) is reported
as **unreachable** (`NetworkFailure`) — a recoverable error that is retried — and
is **never** mistaken for a deleted adapter. Offline ≠ deleted.

A `NotFound` is returned only on a positive, authoritative absence (the node
responded HTTP 200 and the adapter is genuinely not in `/v1/models`), which lets
sync tombstone an out-of-band-unloaded adapter from inventory. Restoration after
such an out-of-band unload is via **re-applying** the source forma (re-apply is
idempotent: it loads if missing, no-ops if present).
