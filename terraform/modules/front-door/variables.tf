variable "resource_group_name" { type = string }
variable "name_prefix" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "origin_hostname" {
  type        = string
  description = "Origin DNS hostname (e.g. ca-api FQDN)"
}

variable "origin_host_header" {
  type        = string
  description = "Host header to send to origin. Usually the same as origin_hostname."
}

variable "custom_domain" {
  type        = string
  description = "Custom domain (e.g. api.agentcanonical.com). Empty = no custom domain."
  default     = ""
}

variable "log_analytics_id" {
  type        = string
  description = "Log Analytics workspace for diagnostic settings"
}
