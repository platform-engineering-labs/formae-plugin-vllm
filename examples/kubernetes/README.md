# Kubernetes example

formae provisions the **entire stack declaratively** against your current
kubeconfig cluster — the vLLM `Deployment` + `PersistentVolumeClaim` + `Service`
(via the [Kubernetes plugin](https://github.com/platform-engineering-labs/formae-plugin-kubernetes))
**and** the LoRA adapters loaded on top (via this plugin). One `formae apply`,
**no `kubectl apply`**.

This is the substrate split in the plugin design — the cluster runs the
inference servers, formae declaratively manages both the workload and what it
serves — except now the workload is formae-managed too, so the whole thing stays
in sync instead of drifting from a one-shot `kubectl apply`.

> **Bring your own cluster.** This example deploys *into* an existing cluster; it
> does not provision one. The `Deployment` requests `nvidia.com/gpu`, so point
> your kubeconfig at a GPU-capable cluster (CoreWeave, GKE, EKS, edge k3s, …).
> Override the context with `--prop context=<name>` or `$K8S_CONTEXT`. To also
> provision the cluster from formae, see `formae-plugin-aws` (EKS) /
> `formae-plugin-gcp` (GKE).

The `vllm-adapters` PVC mounted at `/models/adapters` is the **adapter
distribution layer** — vLLM reads `loraPath` from this volume. formae only
activates (loads/unloads) adapters already present on the PVC.

## Two targets, one forma

`forma.pkl` declares two targets and routes each resource to the right one:

- a **`K8S`** target (kubeconfig auth) for the `Deployment` / `PVC` / `Service`;
- a **`vllm`** target (`baseUrl = http://vllm.default.svc.cluster.local:8000`) for
  the `LoRAAdapter`, pointing at the in-cluster Service the same forma creates.

The k8s schema is versioned per cluster minor: `Config.kubernetesVersion = "1.34"`
pairs with the `@k8s/v1.34/...` imports. Targeting a different version means
bumping both together.

## Why `enableServiceLinks: false`

The Service fronting vLLM is named `vllm` (the natural choice). Kubernetes
uppercases that to the prefix `VLLM_` and injects legacy Docker-style service-link
env vars into every pod in the namespace — including `VLLM_PORT=tcp://<clusterIP>:8000`.
vLLM reads `VLLM_PORT` as its own integer bind port, fails to parse the URI, and the
engine crash-loops (`ValueError: VLLM_PORT '...' appears to be a URI` →
`CrashLoopBackOff`) before it ever downloads a weight.

The Deployment sets `enableServiceLinks = false` on the PodSpec to suppress the
legacy injection. The `vllm` Service name and its DNS (`vllm.default.svc.cluster.local`)
stay intact. If you copy the PodSpec elsewhere, keep this field — or don't name a
Service after an app that reads `<NAME>_PORT`. See the
[vLLM env-var docs](https://docs.vllm.ai/en/stable/serving/env_vars.html).

## Single replica caveat

This runs **one replica**. `POST /v1/load_lora_adapter` only affects the replica
that handled the request — it does **not** propagate across a replica fleet, and a
new/restarted replica won't have the adapter. Scaling out to multiple replicas
needs a fleet controller / router (Ray Serve, AIBrix LoRA controller, vLLM
Production Stack, NVIDIA Dynamo). The plugin already abstracts on
`target = an OpenAI-compatible base_url`, so a multi-replica setup means pointing
the target at a fleet controller/router, with no resource-model change.

## Steps

1. **Point your kubeconfig at the target cluster** (this example uses the current
   context; override with `--prop context=<name>`).

2. **Install both plugins and resolve pkl deps.** The forma needs the `vllm`
   plugin (this repo) and the `k8s` plugin in the agent:

   ```bash
   make install                                   # installs the vllm plugin
   formae plugin install k8s                      # the Kubernetes plugin
   cd examples/kubernetes && pkl project resolve
   ```

3. **Stage the adapter** onto the PVC at `/models/adapters/demo-adapter` (e.g.
   `kubectl cp` into the pod once it's up, or an init job that pulls from a
   per-zone source). This is a data-plane step — formae provisions the PVC but
   does not ship adapter bytes.

4. **Apply the whole stack:**

   ```bash
   formae apply --mode reconcile --simulate examples/kubernetes/forma.pkl
   formae apply --mode reconcile           examples/kubernetes/forma.pkl
   formae inventory
   ```

   formae creates the PVC, Deployment, and Service, then loads the adapter against
   the Service. vLLM takes a little while to pull the image and start serving; if
   the adapter load races a cold start it fails with a network error and formae
   retries, converging once the server is up (a re-apply also converges it).

   Running formae from **outside** the cluster? Port-forward and set the vLLM
   target's `baseUrl` to `http://localhost:8000` in `forma.pkl`:

   ```bash
   kubectl port-forward svc/vllm 8000:8000
   ```

5. **Destroy** when done — one command tears down the whole stack:

   ```bash
   formae destroy examples/kubernetes/forma.pkl
   ```
