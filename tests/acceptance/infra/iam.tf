# Shared by the Serverless namespace and the provisioned cluster. Attach this role to both warehouses.
data "aws_caller_identity" "fixture" {}

resource "aws_iam_role" "this" {
  name = "${substr(var.name_prefix, 0, 55)}-fixture"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"] }
      Action    = "sts:AssumeRole"
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.fixture.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "this" {
  name = "fixture"
  role = aws_iam_role.this.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetBucketLocation", "s3:ListBucket"]
        Resource = module.s3.s3_bucket_arn
      },
      {
        Effect = "Allow"
        Action = ["s3:GetObject"]
        Resource = [
          "${module.s3.s3_bucket_arn}/data/*",
          "${module.s3.s3_bucket_arn}/spectrum/*",
          "${module.s3.s3_bucket_arn}/parquet/*",
          "${module.s3.s3_bucket_arn}/unload/*",
        ]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:PutObject", "s3:AbortMultipartUpload"]
        Resource = "${module.s3.s3_bucket_arn}/unload/*"
      },
      {
        # Live tests own uniquely named tables/partitions in this disposable database, not the database itself.
        Effect = "Allow"
        Action = [
          "glue:GetDatabase", "glue:GetDatabases",
          "glue:CreateTable", "glue:UpdateTable", "glue:DeleteTable", "glue:GetTable", "glue:GetTables",
          "glue:CreatePartition", "glue:BatchCreatePartition", "glue:UpdatePartition", "glue:DeletePartition",
          "glue:BatchDeletePartition", "glue:GetPartition", "glue:GetPartitions", "glue:BatchGetPartition",
        ]
        Resource = [
          local.glue_catalog_arn,
          aws_glue_catalog_database.this.arn,
          "${replace(aws_glue_catalog_database.this.arn, ":database/", ":table/")}/*",
        ]
      },
    ]
  })
}
