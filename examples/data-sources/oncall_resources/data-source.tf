data "oncall_resources" "rbac" {}

output "rbac_resources" {
  value = data.oncall_resources.rbac.resources
}

output "rbac_verbs" {
  value = data.oncall_resources.rbac.verbs
}
