# A SQL UDF and a PL/pgSQL procedure in the consumer-local workspace schema. Both are self-contained, so they need no
# bootstrap tables, and they are dropped before the schema during destroy.
resource "redshift_function" "label" {
  provider    = redshift.consumer
  database    = redshift_schema.local.database
  schema      = redshift_schema.local.name
  name        = "f_example_label"
  arguments   = ["INT", "VARCHAR(64)"]
  return_type = "VARCHAR(128)"
  volatility  = "IMMUTABLE"
  body        = "SELECT $2 || '-' || $1::VARCHAR"
}

data "redshift_function" "label" {
  provider  = redshift.consumer
  database  = redshift_function.label.database
  schema    = redshift_function.label.schema
  name      = redshift_function.label.name
  arguments = redshift_function.label.arguments

  lifecycle {
    postcondition {
      condition = (
        self.signature == "INTEGER, CHARACTER VARYING" && self.return_type == "CHARACTER VARYING" &&
        self.volatility == redshift_function.label.volatility && upper(self.language) == "SQL" &&
        length(self.definition_fingerprint) == 64 && self.definition_fingerprint == redshift_function.label.definition_fingerprint
      )
      error_message = "The label SQL function must report its canonical overload/return type, IMMUTABLE volatility, and catalog body fingerprint."
    }
  }
}

resource "redshift_procedure" "scale" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "sp_example_scale"

  argument {
    name = "factor"
    type = "INTEGER"
  }

  argument {
    name = "amount"
    mode = "INOUT"
    type = "BIGINT"
  }

  argument {
    name = "label"
    mode = "OUT"
    type = "VARCHAR(64)"
  }

  configuration = { search_path = redshift_schema.local.name }
  body          = <<-SQL
    BEGIN
      amount := amount * factor;
      label := f_example_label(factor, 'scaled');
    END;
  SQL

  # The body calls the function at run time; Redshift does not track that dependency, so Terraform orders it.
  depends_on = [redshift_function.label]
}

# The lookup selects the overload by its IN and INOUT types, as OUT arguments are not part of the signature, and
# reports every argument.
data "redshift_procedure" "scale" {
  provider  = redshift.consumer
  database  = redshift_procedure.scale.database
  schema    = redshift_procedure.scale.schema
  name      = redshift_procedure.scale.name
  arguments = [for argument in redshift_procedure.scale.argument : argument.type if argument.mode != "OUT"]

  lifecycle {
    postcondition {
      # SHOW PARAMETERS drops type modifiers and represents IN with a null mode.
      condition = (
        self.signature == "INTEGER, BIGINT" && self.security == "INVOKER" &&
        [for argument in self.argument : argument.name] == ["factor", "amount", "label"] &&
        [for argument in self.argument : coalesce(argument.mode, "IN")] == ["IN", "INOUT", "OUT"] &&
        [for argument in self.argument : argument.type] == ["INTEGER", "BIGINT", "CHARACTER VARYING"] &&
        length(self.definition_fingerprint) == 64 && self.definition_fingerprint == redshift_procedure.scale.definition_fingerprint
      )
      error_message = "The scale procedure must report its IN/INOUT signature, all ordered parameter modes/types, INVOKER security, and catalog body."
    }
  }
}

# The listings report every SQL UDF overload and procedure of the workspace schema. They read after the routines exist,
# because Terraform cannot infer that dependency from the schema filter alone.
data "redshift_functions" "local" {
  provider   = redshift.consumer
  database   = redshift_schema.local.database
  schema     = redshift_schema.local.name
  depends_on = [redshift_function.label]
}

data "redshift_procedures" "local" {
  provider   = redshift.consumer
  database   = redshift_schema.local.database
  schema     = redshift_schema.local.name
  depends_on = [redshift_procedure.scale]
}

# The loader may run the function and pass that right on. Redshift also grants EXECUTE to PUBLIC on creation; this
# tuple owns only the loader's explicit privileges on this overload.
resource "redshift_object_grant" "loader_label" {
  provider                = redshift.consumer
  database_name           = redshift_function.label.database
  schema_name             = redshift_function.label.schema
  object_name             = redshift_function.label.name
  object_type             = "FUNCTION"
  arguments               = join(", ", redshift_function.label.arguments)
  grantee                 = redshift_user.loader.name
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}

data "redshift_object_grant" "loader_label" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.loader_label.database_name
  schema_name   = redshift_object_grant.loader_label.schema_name
  object_name   = redshift_object_grant.loader_label.object_name
  object_type   = redshift_object_grant.loader_label.object_type
  arguments     = redshift_object_grant.loader_label.arguments
  grantee       = redshift_object_grant.loader_label.grantee
  grantee_type  = redshift_object_grant.loader_label.grantee_type

  lifecycle {
    postcondition {
      condition = (
        self.privileges == redshift_object_grant.loader_label.privileges &&
        self.grant_option_privileges == redshift_object_grant.loader_label.grant_option_privileges
      )
      error_message = "The loader must hold exactly EXECUTE with grant option on the label function overload."
    }
  }
}
