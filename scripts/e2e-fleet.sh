#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# e2e-fleet.sh — DOGFOOD 3-node vLLM fleet demo end to end.
# Provisions 3 GPU vLLM boxes (fleet-infra.pkl), declares the adapter set on all
# 3 (fleet.pkl), verifies convergence, drops one node's adapters to show drift,
# restores it, then destroys everything. Billable. Guaranteed teardown via trap.
#
# For the HANDS-OFF auto-reconcile beat, set FORMAE_BINARY to a formae built with
# PLA-5 (#512, in main) and HANDS_OFF=1. On stable formae, the manual re-apply path runs.
set -euo pipefail

FORMAE="${FORMAE_BINARY:-formae}"
REGION="${AWS_REGION:-us-east-1}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EX="$ROOT/examples/aws"
HANDS_OFF="${HANDS_OFF:-0}"

log() { echo "[e2e-fleet] $*"; }
node_ip() { aws ec2 describe-instances --region "$REGION" \
  --filters "Name=tag:Name,Values=vllm-node-$1" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text 2>/dev/null; }
adapters_on() { curl -fsS "http://$1:8000/v1/models" 2>/dev/null | python3 -c "import sys,json;print(sorted(m['id'] for m in json.load(sys.stdin)['data'] if m.get('parent')))"; }

teardown() {
  log "Tearing down fleet + infra..."
  ( cd "$EX" && VLLM_URL_A=x VLLM_URL_B=x "$FORMAE" destroy --yes fleet.pkl ) >/tmp/e2e-fleet-d1.log 2>&1 || true
  ( cd "$EX" && "$FORMAE" destroy --yes fleet-infra.pkl ) >/tmp/e2e-fleet-d2.log 2>&1 || true
  for inst in $(aws ec2 describe-instances --region "$REGION" --filters "Name=tag:vllm-fleet,Values=true" "Name=instance-state-name,Values=running,pending,stopping,stopped" --query 'Reservations[].Instances[].InstanceId' --output text 2>/dev/null); do
    aws ec2 terminate-instances --region "$REGION" --instance-ids "$inst" >/dev/null 2>&1 || true
  done
}
trap teardown EXIT

ensure_vllm() {
  local ip="$1" deadline=$((SECONDS+900))
  until curl -fsS --max-time 5 "http://$ip:8000/v1/models" >/dev/null 2>&1; do
    [ $SECONDS -ge "$deadline" ] && { log "vLLM not ready at $ip"; return 1; }
    sleep 15
  done
}

log "Provisioning fleet infra (3x g4dn)..."
( cd "$EX" && "$FORMAE" apply --mode reconcile --yes fleet-infra.pkl )
IP_A=$(node_ip a); IP_B=$(node_ip b)
log "nodes: A=$IP_A B=$IP_B"
for ip in "$IP_A" "$IP_B"; do ensure_vllm "$ip"; done
export VLLM_URL_A="http://$IP_A:8000" VLLM_URL_B="http://$IP_B:8000"

log "Applying fleet.pkl (declare {chat, jailbreak-detector} on all 3)..."
( cd "$EX" && "$FORMAE" apply --mode reconcile --yes fleet.pkl )

for ip in "$IP_A" "$IP_B"; do
  got=$(adapters_on "$ip")
  log "node $ip adapters: $got"
  echo "$got" | grep -q chat && echo "$got" | grep -q jailbreak-detector || { log "FAIL: node $ip missing adapters"; exit 1; }
done
log "Fleet converged: all 3 nodes serve both adapters."

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
