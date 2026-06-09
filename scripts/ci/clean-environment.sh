#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# clean-environment.sh — teardown hook for conformance tests.
# ===========================================================
# Called before AND after the conformance run (see the Makefile `conformance-test`
# target). For the vLLM plugin the only "cloud resource" to clean up is the real
# vLLM container brought up by scripts/ci/vllm-up.sh, so this removes it.
#
# Idempotent and safe to run when nothing is up (missing container is not an error).
#
# NOTE: this script does NOT touch the GPU AWS dogfood example (examples/aws/),
# which is applied/destroyed manually via formae with explicit go-ahead.

set -euo pipefail

VLLM_CONTAINER="${VLLM_CONTAINER:-vllm-conformance}"

if command -v docker >/dev/null 2>&1; then
  if docker ps -a --filter "name=^${VLLM_CONTAINER}$" --format '{{.Names}}' | grep -q "$VLLM_CONTAINER"; then
    echo "clean-environment.sh: removing vLLM container '${VLLM_CONTAINER}'"
    docker rm -f "$VLLM_CONTAINER" >/dev/null 2>&1 || true
  else
    echo "clean-environment.sh: no '${VLLM_CONTAINER}' container to remove (ok)"
  fi
else
  echo "clean-environment.sh: docker not found; nothing to clean (ok)"
fi

echo "clean-environment.sh: cleanup complete"
