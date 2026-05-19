variable "name" { type = string }
variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "container_apps_environment_id" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "identity_id" { type = string }
variable "image" { type = string }
variable "command" {
  type    = list(string)
  default = []
}

variable "cron_expression" {
  type        = string
  description = "Standard cron: minute hour dom month dow (UTC)"
}

variable "replica_timeout" {
  type    = number
  default = 240
}

variable "retry_limit" {
  type    = number
  default = 1
}

variable "cpu" {
  type    = number
  default = 0.5
}

variable "memory" {
  type    = string
  default = "1Gi"
}

variable "key_vault_id" { type = string }

variable "kv_secret_refs" {
  type    = map(string)
  default = {}
}

variable "env_plain" {
  type    = map(string)
  default = {}
}
