#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# e2e-fleet.sh — DOGFOOD multi-node vLLM fleet demo end to end.
# Provisions the GPU vLLM boxes (fleet-infra.pkl), declares the adapter set on all
# nodes (fleet.pkl), verifies convergence, drops one node's adapters to show drift,
# restores it, then destroys everything. Billable. Guaranteed teardown via trap.
#
# The hands-off auto-reconcile beat (PLA-5 / #512) is included in stable formae
# >= 0.86.1; set HANDS_OFF=1 to exercise it. On older binaries the manual
# re-apply path runs (override the binary with FORMAE_BINARY if needed).
set -euo pipefail

FORMAE="${FORMAE_BINARY:-formae}"
REGION="${AWS_REGION:-us-east-1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EX="$ROOT/examples/aws"
HANDS_OFF="${HANDS_OFF:-0}"

log() { echo "[e2e-fleet] $*"; }
# Dogfood: read the node's public IP out of formae's own inventory (the AWS
# plugin reads it back into the instance resource's properties) rather than
# hitting the EC2 API directly. formae is the source of truth.
node_ip() { "$FORMAE" inventory resources \
  --query "type:AWS::EC2::Instance label:vllm-node-$1" \
  --output-consumer machine --output-schema json 2>/dev/null \
  | jq -r '.Resources[0].ReadOnlyProperties.PublicIp // empty'; }
adapters_on() { curl -fsS "http://$1:8000/v1/models" 2>/dev/null | python3 -c "import sys,json;print(sorted(m['id'] for m in json.load(sys.stdin)['data'] if m.get('parent')))"; }

teardown() {
  log "Tearing down fleet + infra..."
  ( cd "$EX" && "$FORMAE" destroy --yes fleet.pkl ) >/tmp/e2e-fleet-d1.log 2>&1 || true
  ( cd "$EX" && "$FORMAE" destroy --yes fleet-infra.pkl ) >/tmp/e2e-fleet-d2.log 2>&1 || true
  # Backstop: terminate any instance formae's destroy missed (catches an apply
  # that was interrupted before formae recorded the instance). Same tag sweep
  # as the standalone backup script. Billable GPUs — never leak.
  bash "$ROOT/scripts/force-terminate-fleet.sh" || true
  # Print the proof that nothing is left (formae inventory + AWS ground truth).
  bash "$ROOT/scripts/fleet-status.sh" || true
}
trap teardown EXIT

ensure_vllm() {
  local ip="$1" deadline=$((SECONDS+900))
  until curl -fsS --max-time 5 "http://$ip:8000/v1/models" >/dev/null 2>&1; do
    [ $SECONDS -ge "$deadline" ] && { log "vLLM not ready at $ip"; return 1; }
    sleep 15
  done
}

log "Provisioning fleet infra..."
( cd "$EX" && "$FORMAE" apply --mode reconcile --yes fleet-infra.pkl )
IP_A=$(node_ip a); IP_B=$(node_ip b)
log "nodes: A=$IP_A B=$IP_B"
for ip in "$IP_A" "$IP_B"; do ensure_vllm "$ip"; done

# Build the chat-UI image locally — the app stack runs it via the local Docker
# target, wired to node-a's IP + the chat adapter entirely by formae resolvables
# (no VLLM_URL env bridge; the vLLM targets resolve their host from the infra
# instances, and the chat-UI env is wired the same way).
log "Building chat-UI image..."
docker build -q -t formae-chat-ui:demo "$EX/chat-ui" >/dev/null

log "Applying fleet.pkl (adapters + chat-UI, wired by resolvables)..."
( cd "$EX" && "$FORMAE" apply --mode reconcile --yes fleet.pkl )

for ip in "$IP_A" "$IP_B"; do
  got=$(adapters_on "$ip")
  log "node $ip adapters: $got"
  echo "$got" | grep -q chat && echo "$got" | grep -q jailbreak-detector || { log "FAIL: node $ip missing adapters"; exit 1; }
done
log "Fleet converged: every node serves both adapters."

# Verify the cross-plugin graph edge: the chat-UI container's MODEL + VLLM_HOST
# were resolved from the adapter id + node-a's instance IP and injected as compose
# variables. Prove it from the rendered page, then round-trip a prompt.
log "Verifying the chat-UI graph edge (resolvables -> container env)..."
ui_deadline=$((SECONDS+60))
until curl -fsS --max-time 5 http://localhost:8088/healthz >/dev/null 2>&1; do
  [ $SECONDS -ge "$ui_deadline" ] && { log "FAIL: chat-UI not healthy on :8088"; exit 1; }
  sleep 3
done
ui_page=$(curl -fsS http://localhost:8088/ 2>/dev/null || true)
echo "$ui_page" | grep -q "model <b>chat</b>" || { log "FAIL: chat-UI MODEL not resolved to 'chat'"; exit 1; }
echo "$ui_page" | grep -q "$IP_A" || { log "FAIL: chat-UI VLLM_HOST not resolved to node-a IP ($IP_A)"; exit 1; }
log "chat-UI wired by resolvables: model 'chat' @ node-a ($IP_A:8000), live on http://localhost:8088"
reply=$(curl -fsS -X POST http://localhost:8088/api/chat -H 'content-type: application/json' \
  -d '{"message":"hello from the fleet e2e"}' 2>/dev/null || true)
log "chat-UI round-trip reply: $reply"

log "Dropping node B's adapters out-of-band (drift)..."
curl -fsS -X POST "http://$IP_B:8000/v1/unload_lora_adapter" -H 'Content-Type: application/json' -d '{"lora_name":"chat"}' >/dev/null 2>&1 || true
curl -fsS -X POST "http://$IP_B:8000/v1/unload_lora_adapter" -H 'Content-Type: application/json' -d '{"lora_name":"jailbreak-detector"}' >/dev/null 2>&1 || true
log "node B adapters after OOB unload: $(adapters_on "$IP_B")"

if [ "$HANDS_OFF" = "1" ]; then
  log "Waiting for hands-off auto-reconcile to restore node B (needs formae #512)..."
  deadline=$((SECONDS+180))
  until adapters_on "$IP_B" | grep -q jailbreak-detector && adapters_on "$IP_B" | grep -q chat; do
    [ $SECONDS -ge "$deadline" ] && { log "FAIL: auto-reconcile did not restore node B in 180s"; exit 1; }
    sleep 10
  done
  log "Auto-reconcile restored node B hands-off."
else
  log "Restoring node B via re-apply (stable-formae path)..."
  ( cd "$EX" && "$FORMAE" apply --mode reconcile --yes fleet.pkl )
fi
got=$(adapters_on "$IP_B")
echo "$got" | grep -q chat && echo "$got" | grep -q jailbreak-detector || { log "FAIL: node B not restored"; exit 1; }
log "node B restored: $got"
log "E2E PASSED."
