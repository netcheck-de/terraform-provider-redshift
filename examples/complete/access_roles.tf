# Basic SQL roles never depend on SSO; adding an instance ARN enables separate group-mapped roles.
resource "redshift_role" "readers" {
  provider = redshift.consumer
  name     = "example_readers"
}

resource "redshift_role" "operators" {
  provider = redshift.consumer
  name     = "example_operators"
}

# sys:operator covers operational access without full DBA rights; CREATE ROLE is granted in access_grants.tf.
resource "redshift_role_grant" "operators" {
  provider = redshift.consumer
  role     = "sys:operator"
  to_role  = redshift_role.operators.name
}

resource "redshift_role_grant" "reader" {
  provider = redshift.consumer
  role     = redshift_role.readers.name
  to_user  = redshift_user.reader.name
}

data "redshift_role" "readers" {
  provider = redshift.consumer
  name     = redshift_role.readers.name
}

data "redshift_role_grant" "reader" {
  provider = redshift.consumer
  role     = redshift_role_grant.reader.role
  to_user  = redshift_role_grant.reader.to_user
}

data "redshift_role_grant" "operators" {
  provider = redshift.consumer
  role     = redshift_role_grant.operators.role
  to_role  = redshift_role_grant.operators.to_role
}
