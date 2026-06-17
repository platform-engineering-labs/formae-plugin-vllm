# Kubernetes example

vLLM is provisioned by **Kubernetes** (CoreWeave, GKE, EKS, or an edge k3s
cluster); **formae manages the LoRA adapters** against the resulting Service.
This mirrors the substrate split in the plugin design: the cluster runs the
inference servers, formae declaratively manages what they serve.

The `vllm-adapters` PVC mounted at `/models/adapters` is the **adapter
distribution layer** — vLLM reads `loraPath` from this volume. formae only
activates (loads/unloads) adapters that are already present on the PVC.

## Single replica caveat

This manifest runs **one replica**. `POST /v1/load_lora_adapter` only affects the
replica that handled the request — it does **not** propagate across a replica
fleet, and a new/restarted replica won't have the adapter. Scaling out to
multiple replicas needs a fleet controller / router (Ray Serve, AIBrix LoRA
controller, vLLM Production Stack, NVIDIA Dynamo). See the "Scaling &
distribution" section of `docs/superpowers/specs/2026-06-05-vllm-plugin-design.md`.
The plugin already abstracts on `target = an OpenAI-compatible base_url`, so a
multi-replica setup means pointing the target at a fleet controller/router, with
no resource-model change.

## Steps

1. **Provision vLLM:**

   ```bash
   kubectl apply -f vllm-deployment.yaml
   kubectl rollout status deployment/vllm
   ```

2. **Stage the adapter** onto the PVC at `/models/adapters/demo-adapter` (e.g.
   `kubectl cp` into the pod, or an init job that pulls from a per-zone source).

3. **Point the forma target at the Service.** In-cluster, the forma already uses
   `http://vllm.default.svc.cluster.local:8000`. To run formae from outside the
   cluster, port-forward and set the target's `baseUrl` to `http://localhost:8000`:

   ```bash
   kubectl port-forward svc/vllm 8000:8000
   ```

4. **Install the plugin** (from the repo root) and resolve pkl deps:

   ```bash
   make install
   cd examples/kubernetes && pkl project resolve
   ```

5. **Apply:**

   ```bash
   formae apply --mode reconcile --simulate examples/kubernetes/forma.pkl
   formae apply --mode reconcile           examples/kubernetes/forma.pkl
   formae inventory
   ```

6. **Destroy** when done:

   ```bash
   formae destroy examples/kubernetes/forma.pkl
   kubectl delete -f vllm-deployment.yaml
   ```
