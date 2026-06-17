#!/bin/bash
# © 2025 Platform Engineering Labs Inc.
# SPDX-License-Identifier: Apache-2.0
#
# force-terminate-fleet.sh — BACKUP teardown for the fleet demo.
#
# Run this ONLY if `fleet-status.sh` still shows instances after a
# `formae destroy fleet-infra.pkl`. It terminates by the vllm-fleet tag, so it
# catches instances even if formae's destroy didn't (e.g. an apply that was
# interrupted before formae recorded them). Billable GPUs — don't leave leaks.
set -uo pipefail

REGION="${AWS_REGION:-us-east-1}"

ids=$(aws ec2 describe-instances --region "$REGION" \
  --filters "Name=tag:vllm-fleet,Values=true" \
            "Name=instance-state-name,Values=pending,running,stopping,stopped" \
  --query 'Reservations[].Instances[].InstanceId' --output text 2>/dev/null)

if [ -z "${ids// /}" ]; then
  echo "Nothing to terminate — no tagged fleet instances are alive."
  exit 0
fi

echo "Terminating tagged fleet instances: $ids"
# shellcheck disable=SC2086
aws ec2 terminate-instances --region "$REGION" --instance-ids $ids \
  --query 'TerminatingInstances[].[InstanceId,CurrentState.Name]' --output table
