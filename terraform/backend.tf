# Backend pointer — values come from terraform/bootstrap/bootstrap.sh output.
# Pre-init: run bootstrap.sh, then paste the printed values below
# (or `terraform init -backend-config=backend.hcl`).
terraform {
  backend "azurerm" {
    # resource_group_name  = "rg-agent-canonical-tfstate"
    # storage_account_name = "stacwXXXXXX"           # filled by bootstrap.sh
    # container_name       = "tfstate"
    # key                  = "agent-canonical.tfstate"
    # use_azuread_auth     = true
  }
}
