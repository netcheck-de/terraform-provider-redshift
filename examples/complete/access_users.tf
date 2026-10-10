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

  # Sign-in limits and the defaults every new session of the reader starts with.
  valid_until      = "infinity"
  connection_limit = 5
  session_timeout  = 3600
  syslog_access    = "RESTRICTED"
  search_path      = ["$user", "public"]
  session_defaults = {
    timezone          = "UTC"
    statement_timeout = "300000"
  }
}

# An identity without a password signs in only with temporary IAM credentials.
resource "redshift_user" "iam_auditor" {
  provider          = redshift.consumer
  name              = "example_iam_auditor"
  password_disabled = true
  syslog_access     = "UNRESTRICTED"
  connection_limit  = 2
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

# Reading through the membership makes the lookup's member list include the reader.
data "redshift_group" "readers" {
  provider = redshift.consumer
  name     = redshift_group_membership.reader.group
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
