# COPY jobs need an S3 event integration even when AUTO is initially OFF. Tests
# create their own tables/jobs under unique subprefixes of the exported source.
# The S3 module owns the sole bucket policy, including the Redshift service's
# GetBucketNotification, PutBucketNotification and GetBucketLocation permissions.
# Do not separately manage bucket notifications: Redshift installs them itself.
locals {
  # aws_iam_role.this is associated with the namespace and grants ListBucket
  # and GetObject for this prefix, including the tests' nested replacement paths.
  copy_source = "s3://${module.s3.s3_bucket_id}/data/copy/"
}

resource "aws_redshift_resource_policy" "inbound_integrations" {
  resource_arn = aws_redshiftserverless_namespace.this.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "redshift.amazonaws.com" }
        Action    = "redshift:AuthorizeInboundIntegration"
        Resource  = aws_redshiftserverless_namespace.this.arn
        Condition = { StringEquals = { "aws:SourceArn" = module.s3.s3_bucket_arn } }
      },
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.fixture.account_id}:root" }
        Action    = "redshift:CreateInboundIntegration"
        Resource  = aws_redshiftserverless_namespace.this.arn
      },
    ]
  })
}

resource "aws_redshift_integration" "copy" {
  integration_name = "${var.name_prefix}-copy"
  source_arn       = module.s3.s3_bucket_arn
  target_arn       = aws_redshiftserverless_namespace.this.arn

  # A namespace ARN does not imply that its workgroup is ready to receive an integration. Both authorization policies
  # must also exist; the module-level dependency includes the bucket policy, not only the bucket itself.
  depends_on = [
    aws_redshiftserverless_workgroup.this,
    module.s3,
    aws_redshift_resource_policy.inbound_integrations,
  ]
}

resource "redshift_table" "copy" {
  database = var.db_name
  schema   = "public"
  name     = "acc_fixture_copy"

  column {
    name = "id"
    type = "INTEGER"
  }
  column {
    name = "label"
    type = "VARCHAR(64)"
  }
}

resource "redshift_copy_job" "this" {
  database   = redshift_table.copy.database
  name       = "acc_fixture_copy"
  source     = local.copy_source
  iam_role   = aws_iam_role.this.arn
  format     = "CSV"
  auto       = false
  parameters = { IGNOREHEADER = "1" }

  table {
    schema = redshift_table.copy.schema
    name   = redshift_table.copy.name
  }

  # The SQL names only the bucket and role, not the required S3 event integration or attached role policy.
  depends_on = [aws_redshift_integration.copy, aws_iam_role_policy.this]
}

resource "redshift_database" "copy_other" {
  name = "acc_fixture_copy_other"
}
