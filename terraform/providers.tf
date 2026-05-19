provider "azurerm" {
  subscription_id = var.subscription_id
  tenant_id       = var.tenant_id

  features {
    key_vault {
      # Don't actually purge soft-deleted vaults on destroy — production safety
      purge_soft_delete_on_destroy          = false
      recover_soft_deleted_key_vaults       = true
      purge_soft_deleted_secrets_on_destroy = false
      recover_soft_deleted_secrets          = true
    }

    resource_group {
      # Allow destroy of empty RGs only
      prevent_deletion_if_contains_resources = true
    }
  }
}

provider "azuread" {
  tenant_id = var.tenant_id
}

provider "random" {}
