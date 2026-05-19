variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "name_prefix" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "delegated_subnet_id" {
  type        = string
  description = "snet-db ID — must be delegated to Microsoft.DBforPostgreSQL/flexibleServers"
}

variable "private_dns_zone_id" {
  type        = string
  description = "privatelink.postgres.database.azure.com private DNS zone ID"
}

variable "sku_name" {
  type    = string
  default = "B_Standard_B1ms" # Recipe A — 1 vCore, 2 GiB, ~$15/mo
}

variable "storage_mb" {
  type    = number
  default = 32768 # 32 GiB — free tier on B1ms/B2s
}

variable "admin_username" {
  type    = string
  default = "agent_web3"
}

variable "database_name" {
  type    = string
  default = "marketplace"
}

variable "postgres_version" {
  type    = string
  default = "16"
}

variable "key_vault_id" {
  type        = string
  description = "Key Vault to store the admin password in"
}

variable "password_secret_name" {
  type        = string
  description = "Name of the KV secret slot for the PG admin password (raw, for direct DB access)"
}

variable "connection_string_secret_name" {
  type        = string
  description = "Name of the KV secret slot for the full DATABASE_URL the app consumes"
  default     = "postgres-connection-string"
}

variable "backup_retention_days" {
  type    = number
  default = 7
}
