#!/usr/bin/env bash
# bootstrap.sh — create the Terraform remote state backend.
# Idempotent: re-running is safe; only creates resources that don't exist.
set -euo pipefail

LOCATION="${LOCATION:-mexicocentral}"
RG="rg-agent-canonical-tfstate"
CONTAINER="tfstate"

if ! command -v az >/dev/null; then
  echo "az CLI not installed. Install: https://learn.microsoft.com/cli/azure/install-azure-cli"
  exit 1
fi

SUB_ID="$(az account show --query id -o tsv 2>/dev/null || true)"
if [[ -z "${SUB_ID}" ]]; then
  echo "Not logged into az. Run: az login"
  exit 1
fi

echo "Subscription: ${SUB_ID}"
echo "Location:     ${LOCATION}"
echo

# Resource group (idempotent)
if ! az group show --name "${RG}" >/dev/null 2>&1; then
  echo "Creating resource group ${RG}..."
  az group create --name "${RG}" --location "${LOCATION}" >/dev/null
else
  echo "Resource group ${RG} exists, skipping."
fi

# Find or create the storage account. Storage account names must be
# globally unique 3-24 chars lowercase alphanumeric — append a stable
# hash of the subscription ID so re-runs find the same account.
SUFFIX="$(echo -n "${SUB_ID}" | shasum -a 256 | cut -c1-6)"
SA_NAME="stacw${SUFFIX}"

if ! az storage account show --name "${SA_NAME}" --resource-group "${RG}" >/dev/null 2>&1; then
  echo "Creating storage account ${SA_NAME}..."
  az storage account create \
    --name "${SA_NAME}" \
    --resource-group "${RG}" \
    --location "${LOCATION}" \
    --sku Standard_LRS \
    --kind StorageV2 \
    --min-tls-version TLS1_2 \
    --allow-blob-public-access false \
    --public-network-access Enabled \
    --require-infrastructure-encryption true \
    --allow-shared-key-access false \
    >/dev/null

  # Enable blob versioning + 30d soft-delete on the blob service
  az storage account blob-service-properties update \
    --account-name "${SA_NAME}" \
    --resource-group "${RG}" \
    --enable-versioning true \
    --enable-delete-retention true \
    --delete-retention-days 30 \
    >/dev/null
else
  echo "Storage account ${SA_NAME} exists, skipping."
fi

# Container (idempotent). Use RBAC auth, not account key.
if ! az storage container show \
      --account-name "${SA_NAME}" \
      --name "${CONTAINER}" \
      --auth-mode login >/dev/null 2>&1; then
  echo "Creating blob container ${CONTAINER}..."
  az storage container create \
    --account-name "${SA_NAME}" \
    --name "${CONTAINER}" \
    --auth-mode login \
    >/dev/null
else
  echo "Container ${CONTAINER} exists, skipping."
fi

cat <<EOF

Done. Paste this into terraform/backend.tf:

terraform {
  backend "azurerm" {
    resource_group_name  = "${RG}"
    storage_account_name = "${SA_NAME}"
    container_name       = "${CONTAINER}"
    key                  = "agent-canonical.tfstate"
    use_azuread_auth     = true
  }
}

Then: az role assignment create --assignee \$(az ad signed-in-user show --query id -o tsv) \\
        --role "Storage Blob Data Contributor" \\
        --scope /subscriptions/${SUB_ID}/resourceGroups/${RG}/providers/Microsoft.Storage/storageAccounts/${SA_NAME}

Then in terraform/: terraform init
EOF
