terraform {
  # >= 1.11 (oncall_integration's secret_wo).
  required_version = ">= 1.11"

  required_providers {
    oncall = {
      source = "observiply/oncall"
    }
  }
}
provider "oncall" {
  endpoint = "https://oncall.example.com"
  token    = var.oncall_token
}

variable "oncall_token" {
  type      = string
  sensitive = true
}
