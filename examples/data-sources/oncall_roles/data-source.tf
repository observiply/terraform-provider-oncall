data "oncall_roles" "builtin" {}

output "role_ids_by_name" {
  value = { for r in data.oncall_roles.builtin.roles : r.name => r.id }
}
