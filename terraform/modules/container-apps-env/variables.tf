variable "resource_group_name" { type = string }
variable "location" { type = string }
variable "name_prefix" { type = string }
variable "tags" {
  type    = map(string)
  default = {}
}

variable "infrastructure_subnet_id" {
  type        = string
  description = "snet-aca subnet ID — delegated to Microsoft.App/environments"
}

variable "log_retention_days" {
  type    = number
  default = 30
}
