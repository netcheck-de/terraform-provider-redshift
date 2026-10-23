# Owned disposable data needs no access-log bucket or retained versions; SSE-S3 uses an AWS-managed key.
# trivy:ignore:AWS-0089
# trivy:ignore:AWS-0090
# trivy:ignore:AWS-0132
module "fixture_bucket" {
  source    = "terraform-aws-modules/s3-bucket/aws"
  version   = "5.17.0"
  providers = { aws = aws.producer }

  bucket                   = local.fixture_bucket_name
  force_destroy            = true
  control_object_ownership = true
  server_side_encryption_configuration = {
    rule = {
      apply_server_side_encryption_by_default = {
        sse_algorithm = "AES256"
      }
    }
  }

  attach_policy = true
  policy        = local.fixture_bucket_policy
}

locals {
  fixture_bucket_name = "${local.name}-fixture"
  fixture_bucket_policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat([
      {
        Sid       = "RequireTLS"
        Effect    = "Deny"
        Principal = "*"
        Action    = "s3:*"
        Resource  = [module.fixture_bucket.s3_bucket_arn, "${module.fixture_bucket.s3_bucket_arn}/*"]
        Condition = { Bool = { "aws:SecureTransport" = "false" } }
      },
      ], local.cross_account ? tolist([
        {
          Sid       = "ConsumerBucketMetadata"
          Effect    = "Allow"
          Principal = { AWS = aws_iam_role.consumer.arn }
          Action    = ["s3:GetBucketLocation", "s3:ListBucket"]
          Resource  = [module.fixture_bucket.s3_bucket_arn]
        },
        {
          Sid       = "ConsumerFixtureRead"
          Effect    = "Allow"
          Principal = { AWS = aws_iam_role.consumer.arn }
          Action    = ["s3:GetObject"]
          Resource  = ["${module.fixture_bucket.s3_bucket_arn}/data/*", "${module.fixture_bucket.s3_bucket_arn}/unload/*"]
        },
        {
          Sid       = "ConsumerUnload"
          Effect    = "Allow"
          Principal = { AWS = aws_iam_role.consumer.arn }
          Action    = ["s3:PutObject", "s3:AbortMultipartUpload"]
          Resource  = ["${module.fixture_bucket.s3_bucket_arn}/unload/*"]
        },
        ]) : [], local.copy_job_enabled ? tolist([
        {
          Sid       = "RedshiftEventIntegration"
          Effect    = "Allow"
          Principal = { Service = "redshift.amazonaws.com" }
          Action    = ["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]
          Resource  = [module.fixture_bucket.s3_bucket_arn]
          Condition = {
            ArnLike      = { "aws:SourceArn" = "arn:${data.aws_partition.consumer.partition}:redshift:${var.region}:${data.aws_caller_identity.producer.account_id}:integration:*" }
            StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.producer.account_id }
          }
        },
    ]) : [])
  })
}

resource "aws_s3_object" "fixture" {
  provider     = aws.producer
  bucket       = module.fixture_bucket.s3_bucket_id
  key          = "data/fixture.csv"
  content      = "id,label\n1,first\n2,second\n"
  content_type = "text/csv"

  depends_on = [module.fixture_bucket]
}
