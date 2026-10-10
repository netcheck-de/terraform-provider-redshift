# A SQL UDF and a PL/pgSQL procedure in the consumer-local workspace schema. Both are self-contained, so they need no
# bootstrap tables, and they are dropped before the schema during destroy.
resource "redshift_function" "label" {
  provider    = redshift.consumer
  database    = redshift_schema.local.database
  schema      = redshift_schema.local.name
  name        = "f_example_label"
  arguments   = ["int", "varchar(64)"]
  return_type = "varchar(128)"
  volatility  = "IMMUTABLE"
  body        = "SELECT $2 || '-' || $1::varchar"
}

data "redshift_function" "label" {
  provider  = redshift.consumer
  database  = redshift_function.label.database
  schema    = redshift_function.label.schema
  name      = redshift_function.label.name
  arguments = redshift_function.label.arguments
}

resource "redshift_procedure" "scale" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
  name     = "sp_example_scale"

  argument {
    name = "factor"
    type = "integer"
  }
  argument {
    name = "amount"
    mode = "INOUT"
    type = "bigint"
  }
  argument {
    name = "label"
    mode = "OUT"
    type = "varchar(64)"
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
