#!/usr/bin/env bash
# verify-data-plane.sh — prove that the terraform/platform layer is healthy
# before applying terraform/apps.
#
# Usage:
#   scripts/verify-data-plane.sh <env>          # checks postgres + redis + KV; non-zero exit on any failure
#   scripts/verify-data-plane.sh <env> --psql   # additionally adds operator IP to PG firewall, runs SELECT 1, removes the rule

set -euo pipefail

ENV="${1:-}"
EXTRA="${2:-}"

if [[ -z "${ENV}" ]]; then
  echo "usage: $0 <env> [--psql]" >&2
  exit 2
fi

if ! command -v az >/dev/null; then
  echo "az CLI not installed." >&2
  exit 1
fi

RG="rg-agent-canonical-${ENV}"

if ! az group show --name "${RG}" >/dev/null 2>&1; then
  echo "Resource group ${RG} does not exist." >&2
  exit 1
fi

failed=0
section() { printf "\n==> %s\n" "$1"; }

# ---- Postgres ----
section "PostgreSQL Flexible Server"
PG_NAME="$(az postgres flexible-server list --resource-group "${RG}" --query "[0].name" -o tsv)"
if [[ -z "${PG_NAME}" || "${PG_NAME}" == "None" ]]; then
  echo "  FAIL: no postgres server in ${RG}"
  failed=1
else
  PG_STATE="$(az postgres flexible-server show --resource-group "${RG}" --name "${PG_NAME}" --query state -o tsv)"
  PG_FQDN="$(az postgres flexible-server show --resource-group "${RG}" --name "${PG_NAME}" --query fullyQualifiedDomainName -o tsv)"
  echo "  name:  ${PG_NAME}"
  echo "  fqdn:  ${PG_FQDN}"
  echo "  state: ${PG_STATE}"
  [[ "${PG_STATE}" != "Ready" ]] && { echo "  FAIL: postgres state != Ready"; failed=1; }
fi

# ---- Redis ----
section "Redis Cache"
REDIS_NAME="$(az redis list --resource-group "${RG}" --query "[0].name" -o tsv)"
if [[ -z "${REDIS_NAME}" || "${REDIS_NAME}" == "None" ]]; then
  echo "  FAIL: no redis cache in ${RG}"
  failed=1
else
  REDIS_STATE="$(az redis show --resource-group "${RG}" --name "${REDIS_NAME}" --query provisioningState -o tsv)"
  REDIS_HOST="$(az redis show --resource-group "${RG}" --name "${REDIS_NAME}" --query hostName -o tsv)"
  echo "  name:  ${REDIS_NAME}"
  echo "  host:  ${REDIS_HOST}"
  echo "  state: ${REDIS_STATE}"
  [[ "${REDIS_STATE}" != "Succeeded" ]] && { echo "  FAIL: redis provisioningState != Succeeded"; failed=1; }
fi

# ---- Key Vault ----
section "Key Vault"
KV_NAME="$(az keyvault list --resource-group "${RG}" --query "[0].name" -o tsv)"
if [[ -z "${KV_NAME}" || "${KV_NAME}" == "None" ]]; then
  echo "  FAIL: no key vault in ${RG}"
  failed=1
else
  echo "  name:  ${KV_NAME}"

  EXPECTED_AUTOGEN=("postgres-admin-password" "postgres-connection-string" "redis-password" "session-token-secret")
  EXPECTED_OPERATOR=("orchestrator-secret-key" "openrouter-api-key" "tavily-api-key" "auth-free-key" "auth-paid-key" "x402-facilitator-key")
  OPTIONAL=("x402-facilitator-key")
  PLACEHOLDER="PLACEHOLDER_SET_VIA_AZ_KEYVAULT_SECRET_SET"

  is_optional() {
    local s="$1"
    for o in "${OPTIONAL[@]}"; do [[ "$o" == "$s" ]] && return 0; done
    return 1
  }

  for slot in "${EXPECTED_AUTOGEN[@]}" "${EXPECTED_OPERATOR[@]}"; do
    value="$(az keyvault secret show --vault-name "${KV_NAME}" --name "${slot}" --query value -o tsv 2>/dev/null || echo "")"
    if [[ -z "${value}" ]]; then
      if is_optional "${slot}"; then
        echo "  warn: ${slot} missing (optional)"
      else
        echo "  FAIL: ${slot} missing"
        failed=1
      fi
    elif [[ "${value}" == "${PLACEHOLDER}" ]]; then
      if is_optional "${slot}"; then
        echo "  warn: ${slot} placeholder (optional)"
      else
        echo "  FAIL: ${slot} placeholder (run scripts/seed-keyvault.sh ${ENV})"
        failed=1
      fi
    else
      echo "  ok:   ${slot}"
    fi
  done
fi

# ---- Optional psql round-trip ----
# Only works if Postgres has public access enabled. The default Recipe A
# Postgres is VNet-injected (public_network_access_enabled=false), so this
# section will SKIP with a clear note in that case. End-to-end validation
# then happens once ca-api boots and runs migrations from inside the VNet.
if [[ "${EXTRA}" == "--psql" ]]; then
  section "PostgreSQL SELECT 1 round-trip"

  if ! command -v psql >/dev/null; then
    echo "  SKIP: psql not installed"
  elif [[ -z "${PG_NAME:-}" || -z "${KV_NAME:-}" ]]; then
    echo "  SKIP: cannot run without PG_NAME + KV_NAME"
  else
    PG_PUBLIC="$(az postgres flexible-server show --resource-group "${RG}" --name "${PG_NAME}" --query network.publicNetworkAccess -o tsv 2>/dev/null || echo "")"
    if [[ "${PG_PUBLIC}" != "Enabled" ]]; then
      echo "  SKIP: Postgres has public access disabled (VNet-injected). Can only be reached from inside the VNet."
      echo "        End-to-end validation will happen when ca-api boots."
    else
      OP_IP="$(curl -fsS https://api.ipify.org)"
      RULE_NAME="seed-check-$(date +%s)"
      echo "  adding firewall rule ${RULE_NAME} for ${OP_IP}"
      az postgres flexible-server firewall-rule create \
        --resource-group "${RG}" --name "${PG_NAME}" \
        --rule-name "${RULE_NAME}" \
        --start-ip-address "${OP_IP}" --end-ip-address "${OP_IP}" \
        --output none

      trap 'az postgres flexible-server firewall-rule delete --yes --resource-group "${RG}" --name "${PG_NAME}" --rule-name "${RULE_NAME}" --output none || true' EXIT

      DB_URL="$(az keyvault secret show --vault-name "${KV_NAME}" --name postgres-connection-string --query value -o tsv)"
      if PGSSLMODE=require psql "${DB_URL}" -c 'SELECT 1' >/dev/null 2>&1; then
        echo "  ok: SELECT 1 succeeded"
      else
        echo "  FAIL: SELECT 1 failed"
        failed=1
      fi
    fi
  fi
fi

section "Summary"
if [[ "${failed}" -eq 0 ]]; then
  echo "  PASS — data plane is ready for terraform/apps apply"
  exit 0
else
  echo "  FAIL — fix the issues above before applying terraform/apps"
  exit 1
fi
