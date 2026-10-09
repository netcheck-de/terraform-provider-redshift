resource "redshift_role_grant" "operators" {
  role    = "sys:dba"
  to_role = redshift_role.operators.name
}

resource "redshift_role_grant" "grafana" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}
