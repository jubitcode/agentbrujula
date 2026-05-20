#!/usr/bin/env bash
# seed-keyvault.sh — populate operator-managed secret slots in the
# Key Vault provisioned by terraform/platform.
#
# Usage:
#   scripts/seed-keyvault.sh <env>            # interactive: prompts for each unset secret
#   scripts/seed-keyvault.sh <env> --check    # report which slots still hold the placeholder; exit non-zero if any
#   scripts/seed-keyvault.sh <env> --force    # interactive AND overwrite slots that already hold a real value

set -euo pipefail

PLACEHOLDER="PLACEHOLDER_SET_VIA_AZ_KEYVAULT_SECRET_SET"

OPERATOR_SECRETS=(
  "orchestrator-secret-key"
  "openrouter-api-key"
  "tavily-api-key"
  "auth-free-key"
  "auth-paid-key"
  "x402-facilitator-key"
)

usage() {
  sed -n '2,9p' "$0" >&2
  exit 2
}

ENV="${1:-}"
MODE="${2:-seed}"

if [[ -z "${ENV}" ]]; then
  usage
fi

case "${MODE}" in
  seed|--check|--force) ;;
  *) usage ;;
esac

if ! command -v az >/dev/null; then
  echo "az CLI not installed." >&2
  exit 1
fi

RG="rg-agent-canonical-${ENV}"
KV_NAME="$(
  az keyvault list \
    --resource-group "${RG}" \
    --query "[0].name" \
    -o tsv
)"

if [[ -z "${KV_NAME}" || "${KV_NAME}" == "None" ]]; then
  echo "No Key Vault found in ${RG}." >&2
  echo "Has terraform/platform been applied for env=${ENV}?" >&2
  exit 1
fi

echo "Key Vault: ${KV_NAME}"

if [[ "${MODE}" == "--check" ]]; then
  failed=0
  for slot in "${OPERATOR_SECRETS[@]}"; do
    value="$(az keyvault secret show --vault-name "${KV_NAME}" --name "${slot}" --query value -o tsv 2>/dev/null || echo "")"
    if [[ -z "${value}" ]]; then
      echo "MISSING:     ${slot}"
      failed=1
    elif [[ "${value}" == "${PLACEHOLDER}" ]]; then
      echo "PLACEHOLDER: ${slot}"
      failed=1
    else
      echo "OK:          ${slot}"
    fi
  done
  exit "${failed}"
fi

# Seed mode (with optional --force)
for slot in "${OPERATOR_SECRETS[@]}"; do
  current="$(az keyvault secret show --vault-name "${KV_NAME}" --name "${slot}" --query value -o tsv 2>/dev/null || echo "")"

  if [[ -n "${current}" && "${current}" != "${PLACEHOLDER}" && "${MODE}" != "--force" ]]; then
    echo "skip ${slot} (already set; pass --force to overwrite)"
    continue
  fi

  prompt="${slot} value"
  if [[ "${slot}" == *"-key" ]]; then
    prompt="${prompt} (input hidden)"
  fi

  printf "%s: " "${prompt}"
  read -rs new_value
  printf "\n"

  if [[ -z "${new_value}" ]]; then
    echo "  (empty input — skipping ${slot})"
    continue
  fi

  az keyvault secret set \
    --vault-name "${KV_NAME}" \
    --name "${slot}" \
    --value "${new_value}" \
    --output none

  echo "  set ${slot}"
done

echo
echo "Done. Re-run with --check to confirm zero placeholders remain."
