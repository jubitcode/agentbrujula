variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "name_prefix" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "tenant_id" { type = string }

variable "operator_object_id" {
  type        = string
  description = "AAD object ID of the operator who seeds secrets. Gets Key Vault Secrets Officer."
}

variable "workload_identity_principal_id" {
  type        = string
  description = "Principal ID of the user-assigned managed identity Container Apps uses to read secrets."
}

variable "secret_names" {
  type        = list(string)
  description = "Secret slot names to pre-create with placeholder values. Operator overwrites via az keyvault secret set."
  default     = []
}

variable "allowed_ip_ranges" {
  type        = list(string)
  description = "IP CIDRs allowed to hit the vault when network lock-down is active. Empty list = vault is publicly reachable (default_action=Allow, seed phase). Non-empty = default_action=Deny + ip_rules populated."
  default     = []
}

variable "allowed_subnet_ids" {
  type        = list(string)
  description = "Subnet IDs (with Microsoft.KeyVault service endpoint) allowed to reach the vault over the Azure backbone. Container Apps subnet goes here so workload MI can fetch secrets."
  default     = []
}
