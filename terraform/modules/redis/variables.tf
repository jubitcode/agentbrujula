variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "name_prefix" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "subnet_id" {
  type        = string
  description = "snet-redis ID for the private endpoint"
}

variable "private_dns_zone_id" {
  type        = string
  description = "privatelink.redis.cache.windows.net zone ID"
}

variable "sku_name" {
  type    = string
  default = "Basic"
}

variable "capacity" {
  type        = number
  default     = 0 # C0 = 250 MB
  description = "Cache capacity. Basic/Standard 0=C0(250MB), 1=C1(1GB), ..."
}

variable "key_vault_id" {
  type = string
}

variable "password_secret_name" {
  type = string
}
