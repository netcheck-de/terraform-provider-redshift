# The password source and secret version retain the test user's password in Terraform state.
resource "random_password" "reader" {
  length      = 24
  special     = false
  min_lower   = 1
  min_upper   = 1
  min_numeric = 1
}

resource "redshift_user" "reader" {
  provider            = redshift.consumer
  name                = "example_reader"
  password_wo         = random_password.reader.result
  password_wo_version = 1
}

# A loader identity exercises the user capability flags; its password is not exported.
resource "random_password" "loader" {
  length      = 24
  special     = false
  min_lower   = 1
  min_upper   = 1
  min_numeric = 1
}

resource "redshift_user" "loader" {
  provider            = redshift.consumer
  name                = "example_loader"
  password_wo         = random_password.loader.result
  password_wo_version = 1
  superuser           = false
  create_database     = true
}

resource "redshift_group" "readers" {
  provider = redshift.consumer
  name     = "example_reader_group"
}

resource "redshift_group_membership" "reader" {
  provider = redshift.consumer
  group    = redshift_group.readers.name
  user     = redshift_user.reader.name
}

data "redshift_group" "readers" {
  provider = redshift.consumer
  name     = redshift_group.readers.name
}

data "redshift_user" "reader" {
  provider = redshift.consumer
  name     = redshift_user.reader.name
}

data "redshift_group_membership" "reader" {
  provider = redshift.consumer
  group    = redshift_group_membership.reader.group
  user     = redshift_group_membership.reader.user
}
