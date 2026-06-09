# AWS dogfood example

> ⚠️ **This launches billable AWS infrastructure.** A `g4dn.xlarge` GPU instance
> costs roughly **$0.53/hr** (us-east-1, on-demand). Apply manually, only with
> explicit go-ahead, and `formae destroy` when you are done.
>
> `box.pkl` is **NOT YET VALIDATED** against the live AWS plugin schema — verify
> the resource types, field names, the AMI id for your region, and whether
> `userData` must be base64-encoded before you apply.

This example demonstrates the **two-step dogfood flow**:

1. **The formae AWS plugin brings up the GPU box** (`box.pkl`): a `g4dn.xlarge`
   (Tesla T4 — on edge's supported-hardware list), a security group opening
   `tcp/8000`, and user-data that installs Docker and runs the vLLM container with
   `--enable-lora` and `VLLM_ALLOW_RUNTIME_LORA_UPDATING=True`.
2. **The formae vLLM plugin manages the adapter** (`adapter.pkl`): once the box is
   up and serving, the vLLM plugin loads/reconciles the LoRA adapter against the
   box's OpenAI endpoint.

This mirrors the design's non-goal of host provisioning: the vLLM plugin manages
only the vLLM-specific runtime layer; the GPU box is provisioned by the AWS
plugin.

## Steps

```bash
# From the repo root: install the vLLM plugin (the AWS plugin must also be
# installed in your formae environment).
make install
cd examples/aws && pkl project resolve

# Step 1 — bring up the GPU box (BILLABLE). Lock down --allowed-cidr to your IP.
formae apply --mode reconcile --simulate examples/aws/box.pkl
formae apply --mode reconcile examples/aws/box.pkl --allowed-cidr 203.0.113.4/32

# Wait for the box to boot and vLLM to start serving, then find its public IP
# (formae inventory / the AWS console) and:
#   - stage your adapter onto the box at /opt/models/demo-adapter
#   - edit adapter.pkl: set BaseUrl to http://<EC2_PUBLIC_IP>:8000

# Step 2 — manage the adapter on the box.
formae apply --mode reconcile --simulate examples/aws/adapter.pkl
formae apply --mode reconcile examples/aws/adapter.pkl

# Tear everything down.
formae destroy examples/aws/adapter.pkl
formae destroy examples/aws/box.pkl
```
