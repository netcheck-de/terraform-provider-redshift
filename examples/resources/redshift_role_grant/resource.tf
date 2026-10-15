resource "redshift_role_grant" "operators" {
  role    = "sys:dba"
  to_role = redshift_role.operators.name
}

resource "redshift_role_grant" "grafana" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}

# The team lead may grant the readers role to other users and roles.
resource "redshift_role_grant" "team_lead" {
  role         = redshift_role.readers.name
  to_user      = redshift_user.team_lead.name
  admin_option = true
}
