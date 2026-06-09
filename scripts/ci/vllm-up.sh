#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# vllm-up.sh — boot a REAL vLLM OpenAI server (CPU) for conformance tests.
# ========================================================================
# Conformance MUST run against a real vLLM (idempotency, provider-populated
# id/parent/root, path normalization) — never the in-process fake, which only
# backs the integration tests. This script brings up a real vLLM in a CPU-only
# Docker container so conformance works on any host and in CI (no GPU required;
# the LoRA CRUD/discovery contract does not need one).
#
# It is idempotent: re-running tears down any prior container and recreates it.
# `scripts/ci/clean-environment.sh` is the matching teardown (removes the
# container) and is invoked by the Makefile before and after the test run.
#
# Knobs (env):
#   VLLM_PORT        host port to publish (default 8100)
#   VLLM_IMAGE       CPU vLLM image (default: official prebuilt CPU release)
#   VLLM_BASE_MODEL  base model to serve (default Qwen/Qwen2.5-0.5B-Instruct)
#   VLLM_CONTAINER   container name (default vllm-conformance)
#   VLLM_READY_TIMEOUT  seconds to wait for readiness (default 300)

set -euo pipefail

VLLM_PORT="${VLLM_PORT:-8100}"
VLLM_IMAGE="${VLLM_IMAGE:-public.ecr.aws/q9t5s3a7/vllm-cpu-release-repo:latest}"
VLLM_BASE_MODEL="${VLLM_BASE_MODEL:-Qwen/Qwen2.5-0.5B-Instruct}"
VLLM_CONTAINER="${VLLM_CONTAINER:-vllm-conformance}"
VLLM_READY_TIMEOUT="${VLLM_READY_TIMEOUT:-300}"

# Locked model/adapter pair (spec "Model / adapter selection" validation gate):
# a real, public, dimension-matched LoRA for Qwen2.5-0.5B-Instruct (rank 4).
ADAPTER_REPO="${ADAPTER_REPO:-taronklm/Qwen2.5-0.5B-Instruct-lora-chatbot}"

# Repo root = two levels up from this script (scripts/ci/).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ADAPTER_ROOT="$REPO_ROOT/.conformance/adapters"

echo "vllm-up: ensuring real LoRA adapter fixtures..."
# Two adapter directories so the conformance Update step (which changes
# loraPath) is a genuine path change to a second valid adapter.
fetch_adapter() {
  local dir="$1"
  if [ -f "$dir/adapter_config.json" ] && [ -f "$dir/adapter_model.safetensors" ]; then
    return 0
  fi
  mkdir -p "$dir"
  local base="https://huggingface.co/${ADAPTER_REPO}/resolve/main"
  for f in adapter_config.json adapter_model.safetensors; do
    echo "  downloading $f -> $dir"
    curl -fsSL "$base/$f" -o "$dir/$f"
  done
}
fetch_adapter "$ADAPTER_ROOT/qwen-chatbot"
if [ ! -f "$ADAPTER_ROOT/qwen-chatbot-v2/adapter_model.safetensors" ]; then
  mkdir -p "$ADAPTER_ROOT/qwen-chatbot-v2"
  cp "$ADAPTER_ROOT/qwen-chatbot/"* "$ADAPTER_ROOT/qwen-chatbot-v2/"
fi

echo "vllm-up: (re)starting container '$VLLM_CONTAINER' on port $VLLM_PORT..."
docker rm -f "$VLLM_CONTAINER" >/dev/null 2>&1 || true
docker run -d --name "$VLLM_CONTAINER" \
  -p "${VLLM_PORT}:8000" \
  -v "${ADAPTER_ROOT}:/adapters:ro" \
  -v vllm-conformance-hf-cache:/root/.cache/huggingface \
  -e VLLM_ALLOW_RUNTIME_LORA_UPDATING=True \
  -e VLLM_CPU_KVCACHE_SPACE=4 \
  "$VLLM_IMAGE" \
  "$VLLM_BASE_MODEL" \
    --enable-lora --max-lora-rank 16 \
    --dtype bfloat16 --max-model-len 2048 >/dev/null

echo "vllm-up: waiting up to ${VLLM_READY_TIMEOUT}s for vLLM to become ready..."
deadline=$((SECONDS + VLLM_READY_TIMEOUT))
until curl -fsS "http://127.0.0.1:${VLLM_PORT}/v1/models" >/dev/null 2>&1; do
  if ! docker ps --filter "name=^${VLLM_CONTAINER}$" --format '{{.Names}}' | grep -q "$VLLM_CONTAINER"; then
    echo "vllm-up: ERROR container exited during startup. Logs:" >&2
    docker logs --tail 50 "$VLLM_CONTAINER" >&2 || true
    exit 1
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "vllm-up: ERROR timed out after ${VLLM_READY_TIMEOUT}s. Logs:" >&2
    docker logs --tail 50 "$VLLM_CONTAINER" >&2 || true
    exit 1
  fi
  sleep 3
done

echo "vllm-up: READY — vLLM serving '${VLLM_BASE_MODEL}' at http://127.0.0.1:${VLLM_PORT}"
echo "vllm-up: adapter paths inside container: /adapters/qwen-chatbot, /adapters/qwen-chatbot-v2"
