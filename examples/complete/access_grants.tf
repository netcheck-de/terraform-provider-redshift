resource "redshift_grant" "shared_read" {
  for_each = {
    DATABASE = ["USAGE"]
    SCHEMAS  = ["USAGE"]
    TABLES   = ["SELECT"]
  }
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  role          = redshift_role.readers.name
  scope         = each.key
  privileges    = each.value
}

resource "redshift_grant" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "SCHEMA"
  privileges    = ["USAGE"]
}

# Scoped grants inside one schema. FUNCTIONS and PROCEDURES share one Redshift catalog scope, so each
# role/schema tuple uses only one of them.
resource "redshift_grant" "local_schema_tables" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "TABLES"
  privileges    = ["SELECT"]
}

resource "redshift_grant" "local_schema_functions" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "FUNCTIONS"
  privileges    = ["EXECUTE"]
}

resource "redshift_grant" "local_schema_procedures" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.operators.name
  scope         = "PROCEDURES"
  privileges    = ["EXECUTE"]
}

resource "redshift_object_grant" "group_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["USAGE"]
}

resource "redshift_object_grant" "loader_events" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_name   = local.local_table_name
  object_type   = "TABLE"
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["SELECT", "INSERT"]

  depends_on = [aws_redshiftdata_statement.local_table]
}

resource "redshift_object_grant" "operators_database" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  object_type   = "DATABASE"
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["TEMPORARY"]
}

resource "redshift_object_grant" "public_schema_usage" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = "public"
  grantee_type  = "PUBLIC"
  privileges    = ["USAGE"]
}

resource "redshift_system_grant" "operators" {
  provider   = redshift.consumer
  role       = redshift_role.operators.name
  privileges = ["CREATE ROLE"]
}

resource "redshift_assumerole_grant" "reader" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = "DEFAULT"
  grantee      = redshift_role.readers.name
  grantee_type = "ROLE"
  privileges   = ["COPY", "UNLOAD"]

  depends_on = [aws_redshiftdata_statement.assumerole_policy]
}

resource "redshift_default_privileges" "reader_tables" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  owner         = redshift_user.reader.name
  object_type   = "TABLES"
  grantee       = redshift_group.readers.name
  grantee_type  = "GROUP"
  privileges    = ["SELECT"]
}

# An explicit IAM role ARN can be granted to a single user in addition to the role-wide default grant.
resource "redshift_assumerole_grant" "loader" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = aws_iam_role.consumer.arn
  grantee      = redshift_user.loader.name
  grantee_type = "USER"
  privileges   = ["COPY"]

  depends_on = [aws_redshiftdata_statement.assumerole_policy]
}

# Schema-scoped default privileges require the owning user to hold CREATE on that schema.
resource "redshift_object_grant" "loader_schema" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["CREATE", "USAGE"]
}

resource "redshift_default_privileges" "reader_schema_tables" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  owner         = redshift_user.loader.name
  object_type   = "TABLES"
  grantee       = redshift_role.readers.name
  grantee_type  = "ROLE"
  privileges    = ["SELECT"]

  depends_on = [redshift_object_grant.loader_schema]
}

resource "redshift_default_privileges" "operator_routines" {
  for_each      = toset(["FUNCTIONS", "PROCEDURES"])
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  owner         = redshift_user.loader.name
  object_type   = each.key
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["EXECUTE"]
}

data "redshift_grant" "shared_read" {
  for_each      = redshift_grant.shared_read
  provider      = redshift.consumer
  database_name = each.value.database_name
  role          = each.value.role
  scope         = each.value.scope
}

data "redshift_grant" "local_schema" {
  provider      = redshift.consumer
  database_name = redshift_grant.local_schema.database_name
  schema_name   = redshift_grant.local_schema.schema_name
  role          = redshift_grant.local_schema.role
  scope         = redshift_grant.local_schema.scope
}

data "redshift_object_grant" "group_schema" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.group_schema.database_name
  schema_name   = redshift_object_grant.group_schema.schema_name
  object_type   = redshift_object_grant.group_schema.object_type
  grantee       = redshift_object_grant.group_schema.grantee
  grantee_type  = redshift_object_grant.group_schema.grantee_type
}

data "redshift_system_grant" "operators" {
  provider = redshift.consumer
  role     = redshift_system_grant.operators.role
}

data "redshift_assumerole_grant" "reader" {
  provider     = redshift.consumer
  iam_role_arn = var.enable_assumerole_grant ? redshift_assumerole_grant.reader[0].iam_role_arn : "DEFAULT"
  grantee      = var.enable_assumerole_grant ? redshift_assumerole_grant.reader[0].grantee : redshift_role.readers.name
  grantee_type = "ROLE"
}

data "redshift_default_privileges" "reader_tables" {
  provider      = redshift.consumer
  database_name = redshift_default_privileges.reader_tables.database_name
  owner         = redshift_default_privileges.reader_tables.owner
  object_type   = redshift_default_privileges.reader_tables.object_type
  grantee       = redshift_default_privileges.reader_tables.grantee
  grantee_type  = redshift_default_privileges.reader_tables.grantee_type
}

# A user recipient of scoped grants can pass SELECT on, but not INSERT, to others.
resource "redshift_grant" "loader_schema_tables" {
  provider                = redshift.consumer
  database_name           = redshift_schema.local.database
  schema_name             = redshift_schema.local.name
  user                    = redshift_user.loader.name
  scope                   = "TABLES"
  privileges              = ["SELECT", "INSERT"]
  grant_option_privileges = ["SELECT"]
}

resource "redshift_grant" "operator_languages" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  role          = redshift_role.operators.name
  scope         = "LANGUAGES"
  privileges    = ["USAGE"]
}

resource "redshift_grant" "loader_copy_jobs" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  user          = redshift_user.loader.name
  scope         = "COPY JOBS"
  privileges    = ["CREATE", "ALTER", "DROP"]
}

resource "redshift_grant" "reader_templates" {
  provider      = redshift.consumer
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  role          = redshift_role.readers.name
  scope         = "TEMPLATES"
  privileges    = ["USAGE"]
}

# A snapshot covers the table and view that exist in public now; the next apply re-grants objects added later.
resource "redshift_object_grant" "operators_public_tables" {
  provider      = redshift.consumer
  database_name = redshift_database.local.name
  schema_name   = "public"
  object_type   = "ALL TABLES"
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["SELECT"]

  depends_on = [aws_redshiftdata_statement.local_table, aws_redshiftdata_statement.local_view]
}

# The loader may grant SELECT on the fixture table to others.
resource "redshift_object_grant" "reader_events_option" {
  provider                = redshift.consumer
  database_name           = redshift_database.local.name
  schema_name             = "public"
  object_name             = local.local_table_name
  object_type             = "TABLE"
  grantee                 = redshift_user.reader.name
  grantee_type            = "USER"
  privileges              = ["SELECT"]
  grant_option_privileges = ["SELECT"]

  depends_on = [aws_redshiftdata_statement.local_table]
}

# New functions of the loader no longer run for everyone; deleting this tuple restores the Redshift default.
resource "redshift_default_privileges" "loader_functions_public" {
  restore_default_on_destroy = true
  provider                   = redshift.consumer
  database_name              = redshift_database.local.name
  owner                      = redshift_user.loader.name
  object_type                = "FUNCTIONS"
  grantee                    = "public"
  grantee_type               = "PUBLIC"
  privileges                 = []
}

resource "redshift_default_privileges" "loader_tables_reader_option" {
  provider                = redshift.consumer
  database_name           = redshift_database.local.name
  owner                   = redshift_user.loader.name
  object_type             = "TABLES"
  grantee                 = redshift_user.reader.name
  grantee_type            = "USER"
  privileges              = ["SELECT"]
  grant_option_privileges = ["SELECT"]
}

# ON ALL lets the operators role unload with any IAM role once identity-specific ASSUMEROLE control is enabled.
resource "redshift_assumerole_grant" "operators_any_role" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = "ALL"
  grantee      = redshift_role.operators.name
  grantee_type = "ROLE"
  privileges   = ["UNLOAD", "RESULT_OFFLOAD"]

  depends_on = [aws_redshiftdata_statement.assumerole_policy]
}

data "redshift_assumerole_grant" "operators_any_role" {
  provider     = redshift.consumer
  count        = var.enable_assumerole_grant ? 1 : 0
  iam_role_arn = redshift_assumerole_grant.operators_any_role[0].iam_role_arn
  grantee      = redshift_assumerole_grant.operators_any_role[0].grantee
  grantee_type = "ROLE"
}

data "redshift_grant" "loader_schema_tables" {
  provider      = redshift.consumer
  database_name = redshift_grant.loader_schema_tables.database_name
  schema_name   = redshift_grant.loader_schema_tables.schema_name
  user          = redshift_grant.loader_schema_tables.user
  scope         = redshift_grant.loader_schema_tables.scope
}

data "redshift_object_grant" "operators_public_tables" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.operators_public_tables.database_name
  schema_name   = redshift_object_grant.operators_public_tables.schema_name
  object_type   = redshift_object_grant.operators_public_tables.object_type
  grantee       = redshift_object_grant.operators_public_tables.grantee
  grantee_type  = redshift_object_grant.operators_public_tables.grantee_type
}

data "redshift_default_privileges" "loader_functions_public" {
  provider      = redshift.consumer
  database_name = redshift_default_privileges.loader_functions_public.database_name
  owner         = redshift_default_privileges.loader_functions_public.owner
  object_type   = redshift_default_privileges.loader_functions_public.object_type
  grantee       = redshift_default_privileges.loader_functions_public.grantee
  grantee_type  = redshift_default_privileges.loader_functions_public.grantee_type
}

# These explicit consumer tuples belong to the user, not the role-wide scoped grants above.
resource "redshift_object_grant" "shared_reader" {
  for_each = {
    DATABASE = { schema = null, object = null, privileges = ["USAGE"] }
    SCHEMA   = { schema = data.redshift_schema.source.name, object = null, privileges = ["USAGE"] }
    TABLE    = { schema = data.redshift_schema.source.name, object = local.fixture_table_name, privileges = ["SELECT"] }
  }
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = each.value.schema
  object_name   = each.value.object
  object_type   = each.key
  grantee       = redshift_user.reader.name
  grantee_type  = "USER"
  privileges    = each.value.privileges

  depends_on = [redshift_datashare_table.source]
}

data "redshift_object_grant" "shared_reader" {
  for_each      = redshift_object_grant.shared_reader
  provider      = redshift.consumer
  database_name = each.value.database_name
  database_type = each.value.database_type
  schema_name   = each.value.schema_name
  object_name   = each.value.object_name
  object_type   = each.value.object_type
  grantee       = each.value.grantee
  grantee_type  = each.value.grantee_type
}

resource "redshift_object_grant" "shared_bulk_schema" {
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = redshift_schema.bulk_share.name
  object_type   = "SCHEMA"
  grantee       = redshift_user.reader.name
  grantee_type  = "USER"
  privileges    = ["USAGE"]

  depends_on = [redshift_datashare_membership.bulk]
}

# Consumer routine ACLs cover every overload of the shared name, not an argument signature.
resource "redshift_object_grant" "shared_function" {
  provider                = redshift.consumer
  database_name           = redshift_database.shared.name
  database_type           = "shared"
  schema_name             = redshift_schema.bulk_share.name
  object_name             = redshift_function.bulk_share["INTEGER"].name
  object_type             = "FUNCTION"
  grantee                 = redshift_user.reader.name
  grantee_type            = "USER"
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]

  depends_on = [redshift_datashare_membership.bulk]
}

resource "redshift_object_grant" "shared_snapshot_usage" {
  for_each = {
    DATABASE = null
    SCHEMA   = redshift_schema.bulk_share.name
  }
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = each.value
  object_type   = each.key
  grantee       = redshift_user.loader.name
  grantee_type  = "USER"
  privileges    = ["USAGE"]

  depends_on = [redshift_datashare_membership.bulk]
}

# Snapshots use a different recipient from the single-function tuple, avoiding overlapping ownership.
resource "redshift_object_grant" "shared_snapshots" {
  for_each = {
    "ALL TABLES"    = ["SELECT"]
    "ALL FUNCTIONS" = ["EXECUTE"]
  }
  provider                = redshift.consumer
  database_name           = redshift_database.shared.name
  database_type           = "shared"
  schema_name             = redshift_schema.bulk_share.name
  object_type             = each.key
  grantee                 = redshift_user.loader.name
  grantee_type            = "USER"
  privileges              = each.value
  grant_option_privileges = each.value

  depends_on = [redshift_datashare_membership.bulk]
}

data "redshift_object_grant" "shared_function" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.shared_function.database_name
  database_type = "shared"
  schema_name   = redshift_object_grant.shared_function.schema_name
  object_name   = redshift_object_grant.shared_function.object_name
  object_type   = "FUNCTION"
  grantee       = redshift_object_grant.shared_function.grantee
  grantee_type  = "USER"
}

data "redshift_object_grant" "shared_snapshots" {
  for_each      = redshift_object_grant.shared_snapshots
  provider      = redshift.consumer
  database_name = each.value.database_name
  database_type = "shared"
  schema_name   = each.value.schema_name
  object_type   = each.value.object_type
  grantee       = each.value.grantee
  grantee_type  = "USER"
}

# Like the table bootstrap fixtures, this procedure stays inside the owned producer database. Dropping the share
# and then that database cleans up both membership and definition; AWS statement deletion is not a SQL DROP.
resource "aws_redshiftdata_statement" "shared_procedure" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  database           = redshift_database.producer.name
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  sql                = "CREATE OR REPLACE PROCEDURE ${data.redshift_schema.source.name}.sp_shared_grant_example() AS $$ BEGIN RAISE INFO 'shared grant example'; END; $$ LANGUAGE plpgsql"
}

resource "aws_redshiftdata_statement" "share_procedure" {
  provider           = aws.producer
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  database           = redshift_database.producer.name
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  sql                = "GRANT EXECUTE ON PROCEDURE ${data.redshift_schema.source.name}.sp_shared_grant_example() TO DATASHARE ${redshift_datashare.producer.name}"

  depends_on = [aws_redshiftdata_statement.shared_procedure, redshift_datashare_schema.source]
}

resource "redshift_object_grant" "shared_procedure" {
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = data.redshift_schema.source.name
  object_name   = "sp_shared_grant_example"
  object_type   = "PROCEDURE"
  grantee       = redshift_user.reader.name
  grantee_type  = "USER"
  privileges    = ["EXECUTE"]

  depends_on = [aws_redshiftdata_statement.share_procedure]
}

resource "redshift_object_grant" "shared_procedure_usage" {
  for_each = {
    DATABASE = null
    SCHEMA   = data.redshift_schema.source.name
  }
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = each.value
  object_type   = each.key
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["USAGE"]

  depends_on = [aws_redshiftdata_statement.share_procedure]
}

resource "redshift_object_grant" "shared_procedure_snapshot" {
  provider      = redshift.consumer
  database_name = redshift_database.shared.name
  database_type = "shared"
  schema_name   = data.redshift_schema.source.name
  object_type   = "ALL PROCEDURES"
  grantee       = redshift_role.operators.name
  grantee_type  = "ROLE"
  privileges    = ["EXECUTE"]

  depends_on = [aws_redshiftdata_statement.share_procedure]
}

data "redshift_object_grant" "shared_procedure" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.shared_procedure.database_name
  database_type = "shared"
  schema_name   = redshift_object_grant.shared_procedure.schema_name
  object_name   = redshift_object_grant.shared_procedure.object_name
  object_type   = "PROCEDURE"
  grantee       = redshift_object_grant.shared_procedure.grantee
  grantee_type  = "USER"
}

data "redshift_object_grant" "shared_procedure_snapshot" {
  provider      = redshift.consumer
  database_name = redshift_object_grant.shared_procedure_snapshot.database_name
  database_type = "shared"
  schema_name   = redshift_object_grant.shared_procedure_snapshot.schema_name
  object_type   = "ALL PROCEDURES"
  grantee       = redshift_object_grant.shared_procedure_snapshot.grantee
  grantee_type  = "ROLE"
}

# Lock down creation of SQL UDFs while managed, but deliberately restore PUBLIC's default on cleanup.
resource "redshift_language_grant" "public_sql" {
  provider                   = redshift.consumer
  database_name              = redshift_database.local.name
  language_name              = "SQL"
  grantee                    = "public"
  grantee_type               = "PUBLIC"
  privileges                 = []
  restore_default_on_destroy = true
}

data "redshift_language_grant" "public_sql" {
  provider      = redshift.consumer
  database_name = redshift_language_grant.public_sql.database_name
  language_name = redshift_language_grant.public_sql.language_name
  grantee       = redshift_language_grant.public_sql.grantee
  grantee_type  = redshift_language_grant.public_sql.grantee_type
}
