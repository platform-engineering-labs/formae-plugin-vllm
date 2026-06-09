#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# conformance-aws.sh — DOGFOOD real-vLLM conformance on AWS.
# =========================================================
# vLLM LoRA needs a CUDA pinned-memory allocator, so conformance must run against
# a GPU vLLM (CPU-only hosts hit a vLLM bug: is_pin_memory_available() returns
# True off-WSL with no CUDA -> LoRA warmup crashes). GitHub-hosted runners have no
# GPU, so instead we DOGFOOD: formae's own AWS plugin provisions a g4dn (T4) box
# running vLLM, conformance runs against it (VLLM_EXTERNAL), then formae destroys
# everything. Same flow runs locally and in CI.
#
# Requires: the `formae` CLI (or $FORMAE_BINARY), AWS credentials, Docker-free
# (the box runs vLLM, not this host). Installs the aws + vllm plugins as needed.
#
# Teardown is guaranteed via an EXIT trap so a billable box is never leaked.

set -euo pipefail

FORMAE="${FORMAE_BINARY:-formae}"
REGION="${AWS_REGION:-us-east-1}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BOX_DIR="$REPO_ROOT/ci/conformance-aws"
BOX="$BOX_DIR/box.pkl"
APPLY_TIMEOUT="${APPLY_TIMEOUT:-600}"
VLLM_TIMEOUT="${VLLM_TIMEOUT:-900}"
STARTED_AGENT=0

log() { echo "[conformance-aws] $*"; }

cmd_state() {
  "$FORMAE" status command --query="id:$1" --output-consumer machine --output-schema json 2>/dev/null \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['Commands'][0]['State'])" 2>/dev/null || echo "unknown"
}
latest_cmd() { # $1 = apply|destroy
  "$FORMAE" status command --query='client:me' --output-consumer machine --output-schema json 2>/dev/null \
    | python3 -c "import sys,json;cs=[c for c in json.load(sys.stdin)['Commands'] if c['Command']=='$1'];cs.sort(key=lambda c:c.get('StartTs',''));print(cs[-1]['CommandId'] if cs else '')" 2>/dev/null
}
wait_cmd() { # $1=id $2=timeout
  local deadline=$((SECONDS+$2)) s
  while true; do
    s=$(cmd_state "$1")
    case "$s" in Success) return 0;; Failed|Cancelled) log "command $1 -> $s"; return 1;; esac
    [ $SECONDS -ge "$deadline" ] && { log "command $1 timed out ($s)"; return 1; }
    sleep 10
  done
}

teardown() {
  log "Tearing down (formae destroy)..."
  if "$FORMAE" destroy --yes "$BOX" >/tmp/conf-aws-destroy.log 2>&1; then
    local cid; cid=$(latest_cmd destroy)
    [ -n "$cid" ] && wait_cmd "$cid" 300 || true
  else
    log "WARNING: destroy submit failed; check AWS for leftover vllm-conf-* resources:"
    cat /tmp/conf-aws-destroy.log >&2 || true
  fi
  if [ "$STARTED_AGENT" = "1" ]; then "$FORMAE" agent stop >/dev/null 2>&1 || pkill -f 'formae agent start' 2>/dev/null || true; fi
}
trap teardown EXIT

# 1) plugins: aws (to provision) + this plugin (vllm). Install BEFORE the agent
# starts so it loads them. Fail loudly — a missing aws plugin makes the apply hang.
log "Installing aws plugin..."
if ! "$FORMAE" plugin install aws 2>&1 | sed 's/^/[plugin-install] /'; then
  log "ERROR: 'formae plugin install aws' failed"; exit 1
fi
log "Building + installing the vllm plugin..."
make -C "$REPO_ROOT" install >/dev/null
log "Installed plugins:"; "$FORMAE" plugin list 2>&1 | sed 's/^/[plugin-list] /' || true
if ! "$FORMAE" plugin list 2>/dev/null | grep -qiE '(^|[^a-z])aws([^a-z]|$)'; then
  log "ERROR: aws plugin not present after install"; exit 1
fi

# 2) ensure a formae agent is running for apply/destroy.
if ! "$FORMAE" status agent >/dev/null 2>&1; then
  log "Starting formae agent..."
  "$FORMAE" agent start >/tmp/conf-aws-agent.log 2>&1 &
  STARTED_AGENT=1
  for _ in $(seq 1 30); do "$FORMAE" status agent >/dev/null 2>&1 && break; sleep 2; done
  "$FORMAE" status agent >/dev/null 2>&1 || { log "ERROR: agent did not become ready"; cat /tmp/conf-aws-agent.log >&2 || true; exit 1; }
fi

# 3) provision the GPU vLLM box (dogfood the AWS plugin).
log "Provisioning vLLM box via formae (AWS plugin)..."
( cd "$BOX_DIR" && "$FORMAE" apply --mode reconcile --yes "$BOX" ) >/tmp/conf-aws-apply.log 2>&1
APPLY_CID=$(latest_cmd apply)
[ -n "$APPLY_CID" ] || { log "no apply command id"; exit 1; }
wait_cmd "$APPLY_CID" "$APPLY_TIMEOUT" || { cat /tmp/conf-aws-apply.log >&2; exit 1; }

IP=$(aws ec2 describe-instances --region "$REGION" \
  --filters "Name=tag:Name,Values=vllm-conf-box" "Name=instance-state-name,Values=running" \
  --query 'Reservations[0].Instances[0].PublicIpAddress' --output text 2>/dev/null)
[ -n "$IP" ] && [ "$IP" != "None" ] || { log "no instance IP"; exit 1; }
log "Box up at $IP; waiting for vLLM (up to ${VLLM_TIMEOUT}s)..."

deadline=$((SECONDS+VLLM_TIMEOUT))
until curl -fsS --max-time 5 "http://$IP:8000/v1/models" >/dev/null 2>&1; do
  [ $SECONDS -ge "$deadline" ] && { log "vLLM did not become ready"; exit 1; }
  sleep 15
done
log "vLLM ready. Running conformance (VLLM_EXTERNAL) against the box..."

# 4) conformance against the real GPU vLLM. Teardown happens via the trap.
make -C "$REPO_ROOT" conformance-test VLLM_EXTERNAL=1 VLLM_URL="http://$IP:8000" TIMEOUT="${TIMEOUT:-20m}"
log "Conformance PASSED against formae-provisioned GPU vLLM."
