variable "name" {
  type = string
}

variable "resource_group_name" { type = string }
variable "container_apps_environment_id" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "identity_id" {
  type        = string
  description = "User-assigned managed identity resource ID"
}

variable "acr_login_server" {
  type        = string
  description = "ACR hostname for the registry block"
}

variable "image" {
  type        = string
  description = "Full image reference: acr/repo:tag"
}

variable "ingress_external" {
  type        = bool
  default     = true
  description = "true = public, false = internal only (VNet)"
}

variable "target_port" {
  type    = number
  default = 8080
}

variable "cpu" {
  type    = number
  default = 0.5
}

variable "memory" {
  type    = string
  default = "1Gi"
}

variable "min_replicas" {
  type    = number
  default = 1
}

variable "max_replicas" {
  type    = number
  default = 3
}

variable "http_concurrency" {
  type    = number
  default = 100
}

variable "liveness_probe_path" {
  type    = string
  default = ""
}

variable "readiness_probe_path" {
  type    = string
  default = ""
}

variable "command" {
  type    = list(string)
  default = []
}

variable "key_vault_id" {
  type        = string
  description = "Key Vault ID — needed to construct secret references"
}

variable "kv_secret_refs" {
  type        = map(string)
  description = "Map of ENV_VAR_NAME → key vault secret name. Injected as secret env vars."
  default     = {}
}

variable "kv_secret_refs_for_built_strings" {
  type        = map(string)
  description = "Extra KV-backed env vars (e.g. DATABASE_PASSWORD) the app uses at startup."
  default     = {}
}

variable "env_plain" {
  type        = map(string)
  description = "Plain (non-secret) env vars"
  default     = {}
}
