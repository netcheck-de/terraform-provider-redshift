import {
  to = redshift_datashare_privilege.share_admins
  id = jsonencode({
    database_name  = "warehouse"
    datashare_name = "analytics"
    grantee        = "share_admins"
    grantee_type   = "ROLE"
  })
}
