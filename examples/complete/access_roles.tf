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

# The administrator, not the Terraform identity that creates it, owns the audit role.
resource "redshift_role" "auditors" {
  provider = redshift.consumer
  name     = "example_auditors"
  owner    = var.admin_username
}

# The loader may hand the operators role to further users and roles.
resource "redshift_role_grant" "loader_operators" {
  provider     = redshift.consumer
  role         = redshift_role.operators.name
  to_user      = redshift_user.loader.name
  admin_option = true
}

data "redshift_role" "readers" {
  provider = redshift.consumer
  name     = redshift_role.readers.name

  lifecycle {
    postcondition {
      condition     = self.role_id > 0 && self.owner != null && self.owner != ""
      error_message = "The readers role must have a catalog ID and an owning user."
    }
  }
}

data "redshift_role" "operators" {
  provider = redshift.consumer
  name     = redshift_role.operators.name

  lifecycle {
    postcondition {
      condition     = self.role_id > 0 && self.owner != null && self.owner != ""
      error_message = "The operators role must have a catalog ID and an owning user."
    }
  }
}

data "redshift_role" "auditors" {
  provider = redshift.consumer
  name     = redshift_role.auditors.name

  lifecycle {
    postcondition {
      condition     = self.role_id > 0 && self.owner == redshift_role.auditors.owner
      error_message = "The auditors role must exist and be owned by the configured administrator."
    }
  }
}

data "redshift_role_grant" "loader_operators" {
  provider = redshift.consumer
  role     = redshift_role_grant.loader_operators.role
  to_user  = redshift_role_grant.loader_operators.to_user

  lifecycle {
    postcondition {
      condition     = self.exists ? self.admin_option == redshift_role_grant.loader_operators.admin_option : false
      error_message = "The loader must hold the operators role WITH ADMIN OPTION in the catalog."
    }
  }
}

data "redshift_role_grant" "reader" {
  provider = redshift.consumer
  role     = redshift_role_grant.reader.role
  to_user  = redshift_role_grant.reader.to_user

  lifecycle {
    postcondition {
      condition     = self.exists ? self.admin_option == false : false
      error_message = "The reader must hold the readers role without ADMIN OPTION."
    }
  }
}

data "redshift_role_grant" "operators" {
  provider = redshift.consumer
  role     = redshift_role_grant.operators.role
  to_role  = redshift_role_grant.operators.to_role

  lifecycle {
    postcondition {
      condition     = self.exists ? self.admin_option == false : false
      error_message = "The operators role must inherit sys:operator without ADMIN OPTION."
    }
  }
}

data "redshift_roles" "all" {
  provider = redshift.consumer

  depends_on = [redshift_role.readers, redshift_role.operators, redshift_role.auditors]
}

# Every explicit grant of the operators role, to users and roles alike.
data "redshift_role_grants" "operators" {
  provider = redshift.consumer
  role     = redshift_role_grant.loader_operators.role
}
