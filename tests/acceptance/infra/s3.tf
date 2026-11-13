# Disposable fixture data needs neither access logs nor retained versions. SSE-S3 uses an AWS-managed key.
# trivy:ignore:AWS-0089
# trivy:ignore:AWS-0090
# trivy:ignore:AWS-0132
module "s3" {
  source  = "terraform-aws-modules/s3-bucket/aws"
  version = "5.17.0"

  bucket                                = "${substr(var.name_prefix, 0, 30)}-${data.aws_caller_identity.fixture.account_id}-${var.region}"
  force_destroy                         = true
  control_object_ownership              = true
  attach_deny_insecure_transport_policy = true
  attach_policy                         = true
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "redshift.amazonaws.com" }
      Action    = ["s3:GetBucketNotification", "s3:PutBucketNotification", "s3:GetBucketLocation"]
      Resource  = module.s3.s3_bucket_arn
      Condition = {
        StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.fixture.account_id }
        ArnLike      = { "aws:SourceArn" = "arn:aws:redshift:${var.region}:${data.aws_caller_identity.fixture.account_id}:integration:*" }
      }
    }]
  })
  server_side_encryption_configuration = {
    rule = {
      apply_server_side_encryption_by_default = {
        sse_algorithm = "AES256"
      }
    }
  }
}

locals {
  spectrum_location = "s3://${module.s3.s3_bucket_id}/spectrum/events/"
  parquet_location  = "s3://${module.s3.s3_bucket_id}/parquet/"
}

resource "aws_s3_object" "csv" {
  bucket       = module.s3.s3_bucket_id
  key          = "data/fixture.csv"
  content      = "id,label\n1,first\n2,second\n"
  content_type = "text/csv"

}

# The lifecycle test changes event locations but never scans their contents; leave spectrum/ empty.
# Keep the real mapping fixture in a separate prefix so columnar lifecycle tests cannot scan it accidentally.
resource "aws_s3_object" "parquet" {
  bucket         = module.s3.s3_bucket_id
  key            = "parquet/mapping.parquet"
  content_base64 = trimspace(file("${path.module}/samples/mapping.parquet.base64"))
  content_type   = "application/vnd.apache.parquet"

}
