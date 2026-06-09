# Design: formae vLLM plugin (Sector 88 PoC)

**Date:** 2026-06-05
**Status:** Approved design, pre-implementation
**Mode:** Guided (TDD with review checkpoints)

## Context

This is a proof-of-concept formae resource plugin for **vLLM**, built for **Sector 88**
(sector88.co) — a sovereign/edge AI-inference company that runs LLMs on customer-owned,
pre-existing hardware (Jetson Orin, RTX 4090, A100, Tesla T4) in air-gapped / disconnected
environments (defence & intelligence, satellite ground stations, offshore energy, mining).

Sector 88's stack:
- **Runtime** — probes the box, auto-selects a backend (llama.cpp / vLLM / TensorRT-LLM /
  Triton), tiers memory across VRAM→RAM→NVMe, and exposes an OpenAI-compatible endpoint
  (their demo: `localhost:8088/v1/chat/completions`).
- **Hub** — centralized control plane to deploy/monitor/manage a fleet of inference nodes
  across locations. No public API documented as of 2026-06.

vLLM is one of their backends, reached over an OpenAI-compatible HTTP endpoint per node.

## Goal

Demonstrate that formae can **declaratively manage what a vLLM node serves** and
**continuously reconcile + detect out-of-band drift** — the genuinely hard part of running
disconnected edge fleets. The headline capability is dynamic management of **LoRA adapters**
on a running vLLM server.

## Non-goals

- **No host / GPU provisioning.** The hardware pre-exists and is air-gapped. Provisioning is
  delegated — to formae's AWS plugin for the demo box today, and to formae's own host
  provisioning in the future. The vLLM plugin only manages the vLLM-specific runtime layer.
- **No reimplementation of Sector 88's Runtime** (hardware probing, backend selection, memory
  tiering). The plugin assumes vLLM is already running.
- **No model training / fine-tuning.** Adapters are pre-built artifacts referenced by path.

## Architecture

A **runtime/serving** resource plugin. Its target is an already-running vLLM
OpenAI-compatible server on a node. The plugin issues HTTP calls to manage adapters and
observe models. The formae agent runs on-site/on-prem (egress-free), consistent with the
air-gapped deployment model.

```
formae agent (on-prem)
   └── vLLM plugin ── HTTP ──> vLLM OpenAI server (node)  e.g. http://node:8000
                                  ├── GET  /v1/models
                                  ├── POST /v1/load_lora_adapter
                                  └── POST /v1/unload_lora_adapter
```

## Resource model

### LoRAAdapter (full CRUD — headline resource)

A LoRA adapter is a small set of fine-tuning weights layered on a base model. Once loaded,
vLLM exposes it as its **own model id**: consumers call `/v1/chat/completions` with
`"model": "<lora_name>"` and vLLM routes through base + adapter weights.

| Field               | Kind                 | Notes                                                       |
|---------------------|----------------------|-------------------------------------------------------------|
| `lora_name`         | required, CreateOnly | Identity; appears as a model `id` in `/v1/models`           |
| `lora_path`         | required, mutable    | Local filesystem path on the node (air-gapped reality)      |
| `base_model_name`   | optional, CreateOnly | Links the adapter to its base model                         |
| `is_3d_lora_weight` | optional, CreateOnly | MoE adapter layout flag                                     |
| `id`                | ReadOnly             | From `/v1/models` (equals `lora_name`)                      |
| `parent`            | ReadOnly             | Base model id, from `/v1/models`                            |
| `root`              | ReadOnly             | Artifact location reported by vLLM                          |

### Model (read-only / discovery)

The base model served by a node. Set at vLLM **startup**, not via API — so formae can only
**observe/discover** it, never create or update it. It exists in the schema to:
1. Be the **resolvable handle for the base (un-adapted) model** — consumers (apps, routers,
   future Sector 88 Hub integration, other plugins) reference `formae://<model>.id` instead
   of hardcoding the model string, gaining one source of truth + referential integrity.
2. Anchor an adapter's `base_model_name`.
3. Answer "what is this node serving?" during discovery.

| Field | Kind     | Notes                                              |
|-------|----------|----------------------------------------------------|
| `id`  | ReadOnly | Model name; identity. Sourced from `/v1/models`    |
| `root`| ReadOnly | Artifact location                                  |

No Create/Update/Delete. Surfaced via List/discovery (entries in `/v1/models` with no
`parent`).

### Resolvable fields & cross-plugin wiring

- `LoRAAdapter.lora_name` is the resolvable handle for the **fine-tuned** model endpoint.
- `Model.id` is the resolvable handle for the **base** model endpoint.
- The endpoint URL itself comes from the target (`base_url`).

This is how future resources — an app config, an LLM router/gateway, or Sector 88's Hub
registration — would wire themselves to a node's served models.

## Target configuration & auth

| Field      | Required | Notes                                                            |
|------------|----------|------------------------------------------------------------------|
| `base_url` | yes      | e.g. `http://localhost:8000` or `http://<node>:8000`             |
| `api_key`  | no       | Secret; sent as `Authorization: Bearer <key>`. Works with or without vLLM `--api-key`. |

## Plugin configuration

- **RateLimit:** modest default (local/edge server, not a rate-limited cloud API).
- **LabelConfig:** label resources by `lora_name` (adapters) / `id` (models).
- **DiscoveryFilters:** none initially; discovery lists everything in `/v1/models`.

## CRUD → vLLM API mapping

| Op       | vLLM call                                   | Notes                                                              |
|----------|---------------------------------------------|--------------------------------------------------------------------|
| Create   | `POST /v1/load_lora_adapter`                | Body: `lora_name`, `lora_path`, optional `base_model_name`, `is_3d_lora_weight`. |
| Read     | `GET /v1/models`                            | Match `id == lora_name`. Absent ⇒ NotFound (this surfaces drift).  |
| Update   | `POST /v1/load_lora_adapter` `load_inplace=true` | Only `lora_path` is mutable. Name/base change ⇒ replacement (CreateOnly). |
| Delete   | `POST /v1/unload_lora_adapter`              | Body: `lora_name`. Already-gone ⇒ treat as success.                |
| List     | `GET /v1/models`                            | `parent` set ⇒ adapter; no `parent` ⇒ base model.                  |

### Error handling & idempotency

- **NotFound on Read** ⇒ resource absent (drift / needs create).
- **NotFound on Delete** ⇒ success (already unloaded).
- **Already-loaded on Create** ⇒ reconcile via `load_inplace`.
- **Unreachable (transport timeout/refused) ⇒ a DISTINCT error code, never NotFound.**
  See "Offline & failure handling" — mistaking unreachable for absent would let reconcile
  treat an offline node as having had its adapter deleted, which is false and potentially
  destructive.
- Map errors to the SDK's `OperationErrorCode` taxonomy, classified as **transient**
  (unreachable — safe to retry on the next reconcile) vs **permanent** (bad `lora_path`,
  incompatible base, runtime-updating disabled — should surface/alert, not loop forever).
  Requires the server started with `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True` (clear permanent
  error if the endpoint rejects runtime updates).

### Normalization risk

The `root` returned by `/v1/models` may differ from the user-supplied `lora_path` (server
normalization). We preserve user input and compare carefully to avoid false-positive drift.
Final comparison rule validated against real vLLM during implementation.

## Offline & failure handling (fleet behavior) — VERIFIED against formae source

Edge nodes are intermittently connected, so this is first-class, not an edge case. **This
behavior must be tested (fake vLLM) and shown in the demo.** Behavior below confirmed by
reading `formae` source (2026-06-08); see also [[formae-notfound-sync-reconcile]] and PLA-5.

### How the plugin distinguishes "deleted" from "unreachable" (the core rule)

Decided entirely inside the plugin's `Read`, by what it observed over HTTP:

| `Read` observes | returns | classification |
|---|---|---|
| HTTP 200, adapter **not in** `/v1/models` | `ReadResult{ErrorCode: NotFound}`, **nil Go error** | authoritative absence → tombstoned on sync |
| **No HTTP response** (refused/timeout/DNS/TLS) | `ReadResult{ErrorCode: NetworkFailure}` (or `ServiceTimeout`) | recoverable → retried, never tombstoned |
| HTTP 5xx / malformed | `ReadResult{ErrorCode: ServiceInternalError}` | recoverable → retried |
| HTTP 401/403 | `AccessDenied` / `InvalidCredentials` | terminal → fails fast |
| HTTP 429 | `Throttling` | recoverable → retried |

Asymmetry = `NotFound` ONLY on a positive, authoritative absence; no-response is *never*
`NotFound`. There is **no "Unreachable" error code** in formae and we don't need one
(`pkg/plugin/resource/resource.go:143-186`).

⚠️ **Critical implementation rule:** transport failures must be returned as
`ReadResult{ErrorCode: NetworkFailure}` with a **nil Go error** — NOT `return nil, err`. A
non-nil Go error maps to `UnforeseenError` (`plugin_operator.go:513-517`), which is **not
recoverable** → terminal, no retry → breaks offline handling. Needs a dedicated test.

### Why this gives correct fleet behavior

- **Reachable + adapter gone → tombstoned.** Sync treats `NotFound` as success
  (`plugin_operator.go:518-528`) and converts the Read to an `OperationDelete`
  (`resource_persister.go:323-327`) → removed from inventory. This is the OOB-delete
  conformance path (`pkg/plugin-conformance-tests/runner.go:1549-1568`). Our `NotFound`
  mapping makes that test pass.
- **Unreachable → retried, never removed.** `NetworkFailure`/`ServiceTimeout` are recoverable
  (`resource.go:172-186`) and only `NotFound` triggers the Read→Delete conversion → an offline
  node is never tombstoned. "Offline ≠ deleted" for free.
- **`apply` is partial-success across nodes.** Each `LoRAAdapter` is bound to one node-target
  (`base_url`); nodes have no inter-dependencies, so an unreachable node fails *its* resource
  (recoverable, retried up to MaxAttempts, then surfaced as a meaningful
  `node <x> unreachable: <reason>` via a defined API error + CLI renderer) while every
  reachable node converges.

### Restoration / "healing" — current behavior (NOT auto-magic)

Correcting an earlier assumption: a tombstoned adapter does **not** auto-heal on reboot today.
- After an OOB unload on a reachable node, sync removes the adapter from inventory.
- **Restoration requires re-running `formae apply`** on the source forma (re-creates it).
- The per-stack **auto-reconcile policy** exists but reconstructs desired from the `resources`
  table (ACTUAL snapshot), so it cannot resurrect a tombstoned row — it only enforces what's
  still in the snapshot. Hands-off self-heal becomes possible only if formae moves to
  reconstructing desired from `forma_commands`/`resource_updates` (DESIRED) — tracked in
  **PLA-5**, out of scope for this PoC.
- Idempotency holds regardless: re-apply loads if missing, no-ops if present.

## Artifact distribution & storage (out of plugin scope)

The plugin never moves bytes — `lora_path` is read by vLLM from the **node's own filesystem**;
formae only *activates* (loads/unloads) what is already present. Getting artifacts onto nodes
is a separate **distribution** layer (future work; overlaps the config-mgmt/satellite-agents
brainstorm — likely a file/artifact resource or the Sector 88 Hub).

Storage topology within an air-gapped zone (invisible to this plugin — it just reads
`lora_path`):

- vLLM reads `lora_path` **once at load time** into VRAM; inference performance is identical
  for local-disk vs shared-NFS paths. Adapters are also small (tens–hundreds of MB).
- **Shared networked dir** is functionally fine for adapters (one `lora_path`, no per-node
  copy) but is a single point of failure: if it's down, nodes can't (re)load — which breaks
  reboot/reconcile exactly when needed.
- **Local per-node (rsync/registry pull from a per-zone source) to a standardized path**
  (e.g. `/opt/models/adapters/<name>/<version>`) is the resilient default: central
  source-of-truth for distribution, local resilience for activation/reload, easy per-node
  canary. `formae apply` once covers the fleet because the path is standardized.
- **Base models lean local regardless** (GBs; slow to load over NFS at startup; Sector 88's
  NVMe memory-tiering wants local fast storage).

## Scaling & distribution (future-compat)

The PoC is single-GPU (one vLLM server per node). Two future distribution axes, and why the
design already absorbs them:

- **Axis A — model too big for one GPU (scale up):** vLLM tensor-/pipeline-parallel
  (`tensor_parallel_size`/`pipeline_parallel_size`); single-node via multiprocessing, multi-node
  via Ray. Still **one OpenAI endpoint** → invisible to the plugin; it's a server launch flag
  (provisioning layer, out of scope).
- **Axis B — many replicas (scale out):** a router fronting multiple vLLM servers — vLLM
  Production Stack, Ray Serve LLM, NVIDIA Dynamo, llm-d, AIBrix. Also OpenAI-compatible
  endpoint(s).

Because the plugin abstracts on **`target = an OpenAI-compatible base_url`**, the "node" can
later be a multi-GPU server or a router endpoint with no resource-model change. Forward-compat
choices baked in now:

1. Target = OpenAI-compatible endpoint (invariant across both axes).
2. HTTP client stays OpenAI-generic — not coupled to vLLM process internals — so it works
   unchanged against vLLM, a Production-Stack/Dynamo/Ray-Serve router, or AIBrix.

**Known wrinkle (Axis B):** `POST /v1/load_lora_adapter` hits only the replica the LB routed
to — it does NOT propagate across a replica fleet, and a new/restarted replica won't have the
adapter (vLLM RFC #12174). Fleet tools solve it (Ray Serve per-replica LRU
`max_num_adapters_per_replica`; AIBrix LoRA controller; router-level fan-out). The plugin
supports both future strategies without redesign: **target-per-replica**, or **target a fleet
controller/router** (the same "target = Hub/router" endgame already identified). Out of scope
for the PoC; documented so it isn't a surprise.

### Layer 1 — Fake vLLM (hermetic, no GPU)

A small Go HTTP stub (in the spirit of `plugins/fake-aws/`) implementing `/v1/models`,
`/v1/load_lora_adapter`, `/v1/unload_lora_adapter`, including error/NotFound paths and the
"runtime updating disabled" rejection. **All unit, integration (TDD `TestCreate/Read/Update/
Delete/List`), and conformance tests run against it.** In-process for integration tests; a
standalone reachable binary/container for conformance (agent runs out-of-process).

The fake must also be able to simulate **unreachable** (refuse/timeout/hang) so we can test
offline handling: a `TestReadUnreachable` (and apply-against-offline-node) asserting the
plugin returns the distinct *unreachable* (transient) error code — NOT `NotFound` — and that
the meaningful error propagates. This guards the "offline ≠ deleted" rule.

### Layer 2 — Real-model e2e

- **Local (RTX 5090, 32GB):** GPU verified present via `/usr/lib/wsl/lib/nvidia-smi`
  (driver 596.49). Install **NVIDIA Container Toolkit** (`nvidia-ctk runtime configure`) so
  Docker can run the official vLLM container with `--gpus all`. Serve a small ungated model +
  matching LoRA. Fast.
- **AWS (the polished demo):** **dogfood formae's AWS plugin** — a forma brings up a
  `g4dn.xlarge` (Tesla T4 — Sector 88's own listed hardware) with user-data that runs the
  vLLM container; then the vLLM plugin manages the adapter. Brought up only for the demo and
  torn down after. **Explicit user go-ahead required before launching anything billable.**

### Demo script (climax)

1. `formae apply` a forma declaring a `LoRAAdapter` → adapter loads on the node.
2. Query base model vs `model=<lora_name>` → visibly different outputs.
3. **Out-of-band delete:** `curl` unload the adapter directly on the node → `formae` sync
   reads the node (HTTP 200, adapter absent → `NotFound`) and **tombstones** it (gone from
   inventory). Then `formae apply` on the source forma **restores** it. (Shows OOB-delete
   detection + declarative restore — NOT auto-magic; see PLA-5 for hands-off self-heal.)
4. **Offline node = partial-success apply (must show):** with ≥2 nodes, take one offline (stop
   its container / block the port) and `formae apply` → the reachable node converges while the
   offline node reports a clear `unreachable` error (`NetworkFailure`, retried then surfaced)
   and is **not** tombstoned (offline ≠ deleted). Bring the node back and re-run `formae apply`
   → it converges. This is the fleet-convergence story for disconnected edge.
5. `formae destroy` → adapter unloaded.

### TDD loop (per formae plugin tutorial)

For each CRUD op: fetch tutorial page → write integration test (fails: not implemented) →
confirm fails for the right reason → implement minimum code → `make install && go test
-tags=integration ./...` → pass → next op. NEVER implement before the test exists.

## Model / adapter selection

- **Primary base:** `Qwen/Qwen2.5-0.5B-Instruct` (Apache-2.0, ungated), with a public
  Qwen2.5-0.5B adapter (candidate: `taronklm/Qwen2.5-0.5B-Instruct-lora-chatbot`).
- **Fallback base:** `unsloth/Llama-3.2-1B-Instruct` (ungated mirror) + a 1B-matched LoRA.

The LoRA must dimension-match the base. **Validation gate** (not a placeholder): the chosen
pair is locked the first time real vLLM runs — `load_lora_adapter` returns 200 and a chat
completion against `model=<lora_name>` returns 200 with adapter-influenced output. Both bases
are ungated and trivial for a 5090.

## Dev environment notes

- `nvidia-smi` lives at `/usr/lib/wsl/lib/nvidia-smi` (not on `PATH`); GPU passthrough via
  `/dev/dxg` confirmed working.
- The 5090 is Blackwell (sm_120) — use a **recent** vLLM image with Blackwell kernels;
  verify the image's torch supports sm_120 before relying on it.
- Docker present (v29); NVIDIA Container Toolkit to be installed.
- AWS CLI configured (account 226695765433, user `engineering`).

## Build process & milestones

1. Scaffold via `formae plugin init` (during implementation).
2. Schema (PKL): `LoRAAdapter`, `Model` with annotations. *(checkpoint)*
3. Target config + auth. *(checkpoint)*
4. Plugin config (RateLimit, LabelConfig, DiscoveryFilters).
5. Fake vLLM stub.
6. CRUD via TDD: Create → Read → Update → Delete. *(checkpoint per op)*
7. List / discovery (adapters + read-only models). *(checkpoint)*
8. Offline handling: `TestReadUnreachable` + unreachable error code (transient vs permanent). *(checkpoint)*
9. Conformance tests (`make install && make conformance-test`).
10. Local 5090 real-model e2e (incl. offline-node + auto-reconcile heal across ≥2 nodes).
11. AWS dogfood e2e (with go-ahead). *(checkpoint)*
12. Examples (local docker, AWS, **Kubernetes**) + README.

### Examples to ship

- **`examples/local/`** — docker-compose (or run script) starting CPU/GPU vLLM + a forma
  declaring a `LoRAAdapter`.
- **`examples/aws/`** — forma dogfooding the AWS plugin to bring up the GPU box + a forma for
  the adapter.
- **`examples/kubernetes/`** — a vLLM `Deployment` + `Service` manifest (image with
  `--enable-lora` + `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True`, OpenAI port exposed) plus a forma
  whose target `base_url` points at the Service. Demonstrates the CoreWeave/GKE/edge-k3s
  substrate path — vLLM provisioned by k8s, adapters managed by formae.

## Risks

- **Blackwell support** in the chosen vLLM image — mitigate by pinning a recent image and
  verifying early.
- **LoRA/base dimension mismatch** — mitigated by the validation gate above and a fallback pair.
- **Path normalization** producing false drift — resolved by validating the comparison rule
  against real vLLM.
- **Conformance harness expectations** — the fake must faithfully mirror documented vLLM
  responses; refine if the harness assumes behaviors the fake doesn't model.

## Definition of done

- [ ] `LoRAAdapter` full CRUD + `Model` discovery implemented.
- [ ] Offline handling: unreachable returns a distinct transient error code (not `NotFound`),
      with a meaningful surfaced message; covered by `TestReadUnreachable`.
- [ ] Conformance tests pass (`make install && make conformance-test`) against the fake.
- [ ] Local 5090 real-model e2e passes (create, read, update, drift→reconcile, destroy).
- [ ] Offline-node behavior demonstrated across ≥2 nodes: partial-success apply (reachable
      converges, offline reports `unreachable` and is not tombstoned), restored via re-apply
      on reconnect.
- [ ] AWS dogfood e2e passes (with user go-ahead).
- [ ] Working examples in `examples/`: `local/` (docker), `aws/` (dogfood AWS plugin), and
      `kubernetes/` (vLLM Deployment+Service manifest + forma targeting the Service).
- [ ] README: target config, credentials, supported resources, example usage.
