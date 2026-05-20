variable "subscription_id" {
  type        = string
  description = "Azure subscription ID (UUID)."
}

variable "tenant_id" {
  type        = string
  description = "Azure AD tenant ID (UUID)."
}

variable "location" {
  type        = string
  description = "Primary Azure region."
  default     = "eastus2"
}

variable "environment" {
  type        = string
  description = "Environment name (prod / staging). Used in resource names + tags."
  default     = "prod"
}

variable "operator_object_id" {
  type        = string
  description = "AAD object ID of the operator running terraform apply. Gets Key Vault Secrets Officer."
}

variable "keyvault_allowed_ip_ranges" {
  type        = list(string)
  description = "Operator IP CIDRs allowed to hit Key Vault. Empty list = vault publicly reachable for seed phase. Required (non-empty) when environment=prod."
  default     = []

  validation {
    condition     = var.environment != "prod" || length(var.keyvault_allowed_ip_ranges) > 0
    error_message = "keyvault_allowed_ip_ranges must be non-empty when environment=prod."
  }
}

variable "tags" {
  type        = map(string)
  description = "Common tags applied to every resource"
  default = {
    project   = "agent-canonical"
    managedBy = "terraform"
  }
}
