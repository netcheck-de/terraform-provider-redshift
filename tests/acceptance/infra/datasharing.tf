resource "redshift_schema" "source" {
  provider = redshift.producer
  database = var.db_name
  name     = "acc_fixture_source"
}

resource "redshift_table" "source" {
  provider = redshift.producer
  database = redshift_schema.source.database
  schema   = redshift_schema.source.name
  name     = "events"

  column {
    name = "id"
    type = "INTEGER"
  }
}

resource "aws_redshiftdata_statement" "source_seed" {
  cluster_identifier = aws_redshift_cluster.producer.cluster_identifier
  secret_arn         = aws_redshift_cluster.producer.master_password_secret_arn
  database           = redshift_table.source.database
  sql                = <<-SQL
    INSERT INTO "${redshift_table.source.schema}"."${redshift_table.source.name}"
    SELECT 1 WHERE NOT EXISTS (
      SELECT 1 FROM "${redshift_table.source.schema}"."${redshift_table.source.name}"
    )
  SQL
}

resource "redshift_datashare" "this" {
  provider            = redshift.producer
  database            = var.db_name
  name                = "acc_fixture_share"
  publicly_accessible = true
}

resource "redshift_datashare_schema" "this" {
  provider    = redshift.producer
  database    = redshift_datashare.this.database
  datashare   = redshift_datashare.this.name
  schema      = redshift_schema.source.name
  include_new = false

  # INSERT and ALTER DATASHARE can conflict on the source table's catalog; finish seeding before attaching members.
  depends_on = [aws_redshiftdata_statement.source_seed]
}

resource "redshift_datashare_table" "this" {
  provider  = redshift.producer
  database  = redshift_datashare_schema.this.database
  datashare = redshift_datashare_schema.this.datashare
  schema    = redshift_datashare_schema.this.schema
  table     = redshift_table.source.name

}

resource "redshift_datashare_grant" "this" {
  provider     = redshift.producer
  database     = redshift_datashare_table.this.database
  datashare    = redshift_datashare_table.this.datashare
  namespace_id = aws_redshiftserverless_namespace.this.namespace_id
}

resource "redshift_database" "shared" {
  name             = "acc_fixture_shared"
  datashare_arn    = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.this.datashare}"
  with_permissions = true
}
