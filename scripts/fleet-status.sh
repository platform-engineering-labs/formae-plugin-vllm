#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# fleet-status.sh — show the fleet's GPU instances from BOTH formae and AWS.
#
# This is the audience-facing proof for the demo. Run it:
#   * after `apply fleet-infra.pkl`  -> "here they are, formae knows each node + its IP"
#   * after `destroy fleet-infra.pkl` -> "gone — formae's inventory is empty AND AWS agrees"
#
# formae is the source of truth; the AWS query is the independent ground-truth
# cross-check (a skeptical buyer wants to see the cloud confirm it, not just the tool).
set -uo pipefail

REGION="${AWS_REGION:-us-east-1}"
FORMAE="${FORMAE_BINARY:-formae}"

echo "== formae inventory: managed vLLM fleet instances =="
# PublicIp is a read-only/provider-populated field, so it lives in ReadOnlyProperties.
rows=$("$FORMAE" inventory resources \
  --query 'type:AWS::EC2::Instance stack:vllm-fleet-infra' \
  --output-consumer machine --output-schema json 2>/dev/null \
  | jq -r '.Resources[]? | "  \(.Label)\t\(.NativeID)\t\(.ReadOnlyProperties.PublicIp // "?")"')
[ -n "$rows" ] && echo "$rows" || echo "  (none — formae manages zero fleet instances)"

echo
echo "== AWS ground truth: instances tagged vllm-fleet (not terminated) =="
aws ec2 describe-instances --region "$REGION" \
  --filters "Name=tag:vllm-fleet,Values=true" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped,shutting-down" \
  --query 'Reservations[].Instances[].[InstanceId,State.Name,PublicIpAddress]' \
  --output table
echo "(an empty table above = all instances gone)"
