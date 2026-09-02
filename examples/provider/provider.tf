terraform {
  # >= 1.11 for write-only attributes (oncall_integration's secret_wo).
  required_version = ">= 1.11"

  required_providers {
    oncall = {
      source = "observiply/oncall"
    }
  }
}

# endpoint and token are both settable via ONCALL_ENDPOINT / ONCALL_TOKEN instead
# — prefer that over committing a token in a .tf file.
provider "oncall" {
  endpoint = "https://oncall.example.com"
  token    = var.oncall_token
}

variable "oncall_token" {
  type      = string
  sensitive = true
}
