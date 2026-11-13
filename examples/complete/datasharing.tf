locals {
  # The SQL datashare resource exposes no ARN; derive its AWS identity from the cluster namespace ARN.
  datashare_arn = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.consumer.datashare}"
}

resource "redshift_datashare" "producer" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "example_share"
  # A publicly accessible consumer workgroup can only consume shares that allow public access.
  publicly_accessible = var.allow_public_sql
}

data "redshift_datashare" "producer" {
  provider = redshift.producer
  database = redshift_datashare.producer.database
  name     = redshift_datashare.producer.name

  lifecycle {
    postcondition {
      condition     = self.publicly_accessible == redshift_datashare.producer.publicly_accessible && self.share_id > 0
      error_message = "The producer share must exist with its requested public-access policy and a catalog share ID."
    }
  }
}

resource "redshift_datashare_schema" "source" {
  provider    = redshift.producer
  database    = redshift_datashare.producer.database
  datashare   = redshift_datashare.producer.name
  schema      = data.redshift_schema.source.name
  include_new = true

  # Seed first so future-object inclusion cannot race the explicitly managed table membership.
  depends_on = [aws_redshiftdata_statement.source_seed]
}

data "redshift_datashare_schema" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_schema.source.database
  datashare = redshift_datashare_schema.source.datashare
  schema    = redshift_datashare_schema.source.schema

  lifecycle {
    postcondition {
      condition     = self.exists && self.include_new == redshift_datashare_schema.source.include_new
      error_message = "The producer's public schema must be shared with future-object inclusion enabled."
    }
  }
}

resource "redshift_datashare_table" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_schema.source.database
  datashare = redshift_datashare_schema.source.datashare
  schema    = redshift_datashare_schema.source.schema
  table     = local.fixture_table_name

  depends_on = [aws_redshiftdata_statement.source_seed]
}

data "redshift_datashare_table" "source" {
  provider  = redshift.producer
  database  = redshift_datashare_table.source.database
  datashare = redshift_datashare_table.source.datashare
  schema    = redshift_datashare_table.source.schema
  table     = redshift_datashare_table.source.table

  lifecycle {
    postcondition {
      condition     = self.exists
      error_message = "The seeded producer fixture table must be an explicit member of the producer share."
    }
  }
}

# Observe automatic and explicit membership without claiming ownership of bulk-added objects or UDF overloads.
data "redshift_datashare_schemas" "producer" {
  provider  = redshift.producer
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name

  depends_on = [redshift_datashare_schema.source, redshift_datashare_schema.bulk]

  lifecycle {
    postcondition {
      condition = alltrue([for expected in [redshift_datashare_schema.source, redshift_datashare_schema.bulk] :
        anytrue([for observed in self.datashare_schemas : observed.schema == expected.schema && observed.include_new == expected.include_new])
      ])
      error_message = "The producer share must contain both source schemas with their distinct include_new policies."
    }
  }
}

data "redshift_datashare_tables" "producer" {
  provider  = redshift.producer
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name

  depends_on = [redshift_datashare_table.source, redshift_datashare_membership.bulk]
}

# Bulk owners use a dedicated source schema: no overlap with the explicit table above or its include_new policy.
resource "redshift_schema" "bulk_share" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "example_bulk_share"
}

resource "redshift_table" "bulk_share" {
  provider = redshift.producer
  database = redshift_schema.bulk_share.database
  schema   = redshift_schema.bulk_share.name
  name     = "events"

  column {
    name = "id"
    type = "INTEGER"
  }
}

resource "redshift_function" "bulk_share" {
  for_each    = toset(["INTEGER", "VARCHAR"])
  provider    = redshift.producer
  database    = redshift_schema.bulk_share.database
  schema      = redshift_schema.bulk_share.name
  name        = "f_shared_identity"
  arguments   = [each.key]
  return_type = each.key
  volatility  = "IMMUTABLE"
  body        = "SELECT $1"
}

resource "redshift_datashare_schema" "bulk" {
  provider    = redshift.producer
  database    = redshift_datashare.producer.database
  datashare   = redshift_datashare.producer.name
  schema      = redshift_schema.bulk_share.name
  include_new = false
}

resource "redshift_datashare_membership" "bulk" {
  for_each  = toset(["TABLES", "FUNCTIONS"])
  provider  = redshift.producer
  database  = redshift_datashare_schema.bulk.database
  datashare = redshift_datashare_schema.bulk.datashare
  schema    = redshift_datashare_schema.bulk.schema
  kind      = each.key
  all       = true

  # Both overloaded SQL UDFs and the table exist before exclusive coverage is established.
  depends_on = [redshift_table.bulk_share, redshift_function.bulk_share]
}

data "redshift_datashare_membership" "bulk" {
  for_each  = redshift_datashare_membership.bulk
  provider  = redshift.producer
  database  = each.value.database
  datashare = each.value.datashare
  schema    = each.value.schema
  kind      = each.value.kind

  lifecycle {
    postcondition {
      condition     = self.exists && self.all == each.value.all
      error_message = "The bulk share must cover every current ${each.key} member, including both function overloads."
    }
  }
}

# Lists coverage of both kinds for every schema of the share, including the include_new schema with its explicit table.
data "redshift_datashare_memberships" "producer" {
  provider  = redshift.producer
  database  = redshift_datashare.producer.database
  datashare = redshift_datashare.producer.name

  depends_on = [redshift_datashare_membership.bulk, redshift_datashare_table.source]
}

# Same-account sharing is namespace-scoped; cross-account mode uses an account grant plus AWS authorization.
resource "redshift_datashare_grant" "consumer" {
  provider     = redshift.producer
  database     = redshift_datashare.producer.database
  datashare    = redshift_datashare.producer.name
  account_id   = local.cross_account ? data.aws_caller_identity.consumer.account_id : null
  namespace_id = local.cross_account ? null : aws_redshiftserverless_namespace.consumer.namespace_id

  depends_on = [redshift_datashare_table.source]
}

data "redshift_datashare_grant" "consumer" {
  provider     = redshift.producer
  database     = redshift_datashare_grant.consumer.database
  datashare    = redshift_datashare_grant.consumer.datashare
  account_id   = redshift_datashare_grant.consumer.account_id
  namespace_id = redshift_datashare_grant.consumer.namespace_id

  lifecycle {
    postcondition {
      condition     = self.exists
      error_message = "The producer share must grant USAGE to the configured consumer account or namespace."
    }
  }
}

data "redshift_datashare_grants" "producer" {
  provider  = redshift.producer
  datashare = redshift_datashare_grant.consumer.datashare
}

resource "aws_redshift_data_share_authorization" "consumer" {
  provider            = aws.producer
  count               = local.cross_account ? 1 : 0
  consumer_identifier = data.aws_caller_identity.consumer.account_id
  data_share_arn      = local.datashare_arn
}

resource "aws_redshift_data_share_consumer_association" "this" {
  provider       = aws.consumer
  count          = local.cross_account ? 1 : 0
  data_share_arn = aws_redshift_data_share_authorization.consumer[0].data_share_arn
  consumer_arn   = aws_redshiftserverless_namespace.consumer.arn
  allow_writes   = false
}

resource "redshift_database" "shared" {
  provider      = redshift.consumer
  name          = "example_shared"
  datashare_arn = local.datashare_arn

  # A fresh association can take a while to reach the SQL catalog.
  timeouts {
    create = "15m"
  }

  depends_on = [aws_redshift_data_share_consumer_association.this]
}

data "redshift_database" "shared" {
  provider = redshift.consumer
  name     = redshift_database.shared.name

  lifecycle {
    postcondition {
      condition = (
        upper(self.database_type) == "SHARED" && self.datashare_arn == redshift_database.shared.datashare_arn &&
        self.share_name == redshift_datashare.producer.name && self.with_permissions == redshift_database.shared.with_permissions
      )
      error_message = "The consumer database must be backed by the requested producer datashare with the configured permissions mode."
    }
  }
}

# A second share receives schema permissions through scoped grants instead of explicit membership resources.
# Grants and membership resources must not manage the same share/schema tuple, so this share stays separate.
resource "redshift_datashare" "grants" {
  provider = redshift.producer
  database = redshift_database.producer.name
  name     = "example_share_grants"
}

data "redshift_datashare" "grants" {
  provider = redshift.producer
  database = redshift_datashare.grants.database
  name     = redshift_datashare.grants.name

  lifecycle {
    postcondition {
      condition     = self.share_id > 0 && self.publicly_accessible == false
      error_message = "The scoped-grants share must exist without public access."
    }
  }
}

resource "redshift_grant" "share_schema" {
  for_each = {
    SCHEMA = ["USAGE"]
    TABLES = ["SELECT"]
  }
  provider      = redshift.producer
  database_name = redshift_datashare.grants.database
  schema_name   = data.redshift_schema.source.name
  datashare     = redshift_datashare.grants.name
  scope         = each.key
  privileges    = each.value

  depends_on = [aws_redshiftdata_statement.source_table]
}

data "redshift_grant" "share_schema" {
  provider      = redshift.producer
  database_name = redshift_grant.share_schema["TABLES"].database_name
  schema_name   = redshift_grant.share_schema["TABLES"].schema_name
  datashare     = redshift_grant.share_schema["TABLES"].datashare
  scope         = redshift_grant.share_schema["TABLES"].scope

  lifecycle {
    postcondition {
      condition     = self.privileges == redshift_grant.share_schema["TABLES"].privileges
      error_message = "The grants share must hold exactly the configured scoped SELECT privileges on public tables."
    }
  }
}

data "redshift_grant" "share_schema_usage" {
  provider      = redshift.producer
  database_name = redshift_grant.share_schema["SCHEMA"].database_name
  schema_name   = redshift_grant.share_schema["SCHEMA"].schema_name
  datashare     = redshift_grant.share_schema["SCHEMA"].datashare
  scope         = redshift_grant.share_schema["SCHEMA"].scope

  lifecycle {
    postcondition {
      condition     = self.privileges == redshift_grant.share_schema["SCHEMA"].privileges
      error_message = "The grants share must hold the configured USAGE privilege on public."
    }
  }
}

# A producer-side role administers the grants share without owning it: ALTER changes its objects, SHARE its consumers.
resource "redshift_role" "share_operators" {
  provider = redshift.producer
  name     = "example_share_operators"
}

data "redshift_role" "share_operators" {
  provider = redshift.producer
  name     = redshift_role.share_operators.name

  lifecycle {
    postcondition {
      condition     = self.role_id > 0 && self.owner != null && self.owner != ""
      error_message = "The producer share-operator role must have a visible catalog ID and owner."
    }
  }
}

resource "redshift_datashare_privilege" "share_operators" {
  provider       = redshift.producer
  database_name  = redshift_datashare.grants.database
  datashare_name = redshift_datashare.grants.name
  grantee_type   = "ROLE"
  grantee        = redshift_role.share_operators.name
  privileges     = ["ALTER", "SHARE"]
}

data "redshift_datashare_privilege" "share_operators" {
  provider       = redshift.producer
  database_name  = redshift_datashare_privilege.share_operators.database_name
  datashare_name = redshift_datashare_privilege.share_operators.datashare_name
  grantee_type   = redshift_datashare_privilege.share_operators.grantee_type
  grantee        = redshift_datashare_privilege.share_operators.grantee

  lifecycle {
    postcondition {
      condition     = self.privileges == redshift_datashare_privilege.share_operators.privileges
      error_message = "The share-operator role must hold exactly ALTER and SHARE on the scoped-grants share."
    }
  }
}

data "redshift_datashare_privileges" "grants" {
  provider       = redshift.producer
  datashare_name = redshift_datashare_privilege.share_operators.datashare_name
}

# Lake Formation consumers receive usage VIA DATA CATALOG and an AWS authorization for the DataCatalog/ identifier.
resource "redshift_datashare_grant" "lake_formation" {
  provider         = redshift.producer
  count            = var.lake_formation_account_id == null ? 0 : 1
  database         = redshift_datashare.grants.database
  datashare        = redshift_datashare.grants.name
  account_id       = var.lake_formation_account_id
  via_data_catalog = true

  depends_on = [redshift_grant.share_schema]
}

data "redshift_datashare_grant" "lake_formation" {
  count            = length(redshift_datashare_grant.lake_formation)
  provider         = redshift.producer
  database         = redshift_datashare_grant.lake_formation[count.index].database
  datashare        = redshift_datashare_grant.lake_formation[count.index].datashare
  account_id       = redshift_datashare_grant.lake_formation[count.index].account_id
  via_data_catalog = redshift_datashare_grant.lake_formation[count.index].via_data_catalog

  lifecycle {
    postcondition {
      condition     = self.exists
      error_message = "The optional Lake Formation account must have its VIA DATA CATALOG share grant."
    }
  }
}

# Bulk coverage is vacuously true for an empty source set, so independently observe the helper objects as well.
data "redshift_schemas" "producer" {
  provider = redshift.producer
  database = redshift_schema.bulk_share.database

  lifecycle {
    postcondition {
      condition     = contains([for schema in self.schemas : schema.name], redshift_schema.bulk_share.name)
      error_message = "Producer schema discovery must find the isolated bulk-share helper schema."
    }
  }
}

data "redshift_tables" "bulk_share" {
  provider = redshift.producer
  database = redshift_table.bulk_share.database
  schema   = redshift_table.bulk_share.schema

  lifecycle {
    postcondition {
      condition     = contains([for table in self.tables : table.name], redshift_table.bulk_share.name)
      error_message = "Bulk-share source discovery must find the managed events helper table."
    }
  }
}

data "redshift_functions" "bulk_share" {
  provider = redshift.producer
  database = redshift_schema.bulk_share.database
  schema   = redshift_schema.bulk_share.name

  depends_on = [redshift_function.bulk_share]

  lifecycle {
    postcondition {
      condition = alltrue([for signature in ["INTEGER", "CHARACTER VARYING"] : anytrue([
        for function in self.functions : function.name == "f_shared_identity" && function.signature == signature &&
        function.return_type == signature && function.volatility == "IMMUTABLE" && upper(function.language) == "SQL"
      ])])
      error_message = "Bulk-share function discovery must find both IMMUTABLE SQL identity overloads with their canonical return types."
    }
  }
}

resource "aws_redshift_data_share_authorization" "lake_formation" {
  provider            = aws.producer
  count               = var.lake_formation_account_id == null ? 0 : 1
  data_share_arn      = "${replace(aws_redshift_cluster.producer.cluster_namespace_arn, ":namespace:", ":datashare:")}/${redshift_datashare_grant.lake_formation[0].datashare}"
  consumer_identifier = "DataCatalog/${redshift_datashare_grant.lake_formation[0].account_id}"
}

# Listings observe both sides of the share: the producer's outbound shares and the consumer's inbound share.
data "redshift_datashares" "outbound" {
  provider = redshift.producer

  depends_on = [redshift_datashare.producer, redshift_datashare.grants]
}

data "redshift_inbound_datashares" "consumer" {
  provider = redshift.consumer
  name     = redshift_datashare.producer.name

  depends_on = [redshift_database.shared]
}
