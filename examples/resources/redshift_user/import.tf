import {
  to = redshift_user.grafana
  id = jsonencode({
    workgroup_name = "warehouse"
    database       = "admin"
    name           = "grafana"
  })
}
