data "oncall_teams" "all" {
  scope = "all"
}

output "team_names" {
  value = [for t in data.oncall_teams.all.teams : t.name]
}
