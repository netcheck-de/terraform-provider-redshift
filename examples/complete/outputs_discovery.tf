output "discovery" {
  description = "Catalog listings of the example's own databases, schemas, fixture table, columns, and primary key."
  value = {
    local_databases = [for database in data.redshift_databases.example.databases : database.name]
    local_schemas   = [for schema in data.redshift_schemas.local.schemas : schema.name]
    fixture_tables  = [for table in data.redshift_tables.fixture.tables : "${table.schema}.${table.name}"]
    fixture_columns = { for column in data.redshift_columns.fixture.columns : column.name => column.data_type }
    primary_keys    = { for key in data.redshift_constraints.discovery_keys.constraints : key.name => key.columns }
    key_comment     = redshift_comment.discovery_key.text
  }
}
