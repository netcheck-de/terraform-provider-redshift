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

  lifecycle {
    postcondition {
      condition     = self.group_id > 0 && contains(self.members, redshift_group_membership.reader.user)
      error_message = "The reader group must exist in the catalog and contain the managed reader membership."
    }
  }
}

data "redshift_user" "reader" {
  provider = redshift.consumer
  name     = redshift_user.reader.name

  lifecycle {
    postcondition {
      condition = (
        !self.superuser && !self.create_database && self.valid_until == null &&
        self.connection_limit == redshift_user.reader.connection_limit &&
        self.session_timeout == redshift_user.reader.session_timeout &&
        self.syslog_access == redshift_user.reader.syslog_access &&
        self.search_path == redshift_user.reader.search_path &&
        self.session_defaults == redshift_user.reader.session_defaults
      )
      error_message = "The reader catalog flags, non-expiring password, sign-in limits, and stored session defaults must match."
    }
  }
}

# Neither password contents nor PASSWORD DISABLE have a documented catalog readback. Verify only observable flags/options.
data "redshift_user" "iam_auditor" {
  provider = redshift.consumer
  name     = redshift_user.iam_auditor.name

  lifecycle {
    postcondition {
      condition = (
        !self.superuser && !self.create_database && self.valid_until == null &&
        self.connection_limit == redshift_user.iam_auditor.connection_limit &&
        self.syslog_access == redshift_user.iam_auditor.syslog_access
      )
      error_message = "The IAM auditor must have the configured catalog capabilities, connection limit, and syslog access."
    }
  }
}

data "redshift_user" "loader" {
  provider = redshift.consumer
  name     = redshift_user.loader.name

  lifecycle {
    postcondition {
      condition = (
        self.superuser == redshift_user.loader.superuser &&
        self.create_database == redshift_user.loader.create_database && self.valid_until == null
      )
      error_message = "The loader's catalog capabilities and non-expiring password validity must match the managed settings."
    }
  }
}

data "redshift_group_membership" "reader" {
  provider = redshift.consumer
  group    = redshift_group_membership.reader.group
  user     = redshift_group_membership.reader.user

  lifecycle {
    postcondition {
      condition     = self.exists
      error_message = "The reader's explicit group membership must exist in the catalog."
    }
  }
}

# Warehouse-wide listings; the reader appears once its group membership exists.
data "redshift_users" "all" {
  provider = redshift.consumer

  depends_on = [redshift_user.reader, redshift_user.iam_auditor, redshift_user.loader]
}

data "redshift_groups" "all" {
  provider = redshift.consumer

  depends_on = [redshift_group_membership.reader]
}

data "redshift_group_memberships" "readers" {
  provider = redshift.consumer
  group    = redshift_group_membership.reader.group
}
