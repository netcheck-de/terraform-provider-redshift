resource "redshift_user" "grafana" {
  name                = "grafana"
  password_wo         = random_password.grafana.result
  password_wo_version = 0
}

resource "redshift_role_grant" "grafana_monitor" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name
}
