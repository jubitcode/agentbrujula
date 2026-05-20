terraform {
  backend "azurerm" {
    resource_group_name  = "rg-agent-canonical-tfstate"
    storage_account_name = "stacwa62d62"
    container_name       = "tfstate"
    key                  = "platform.tfstate"
    use_azuread_auth     = true
  }
}
