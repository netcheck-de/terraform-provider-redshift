resource "aws_iam_role" "producer" {
  provider = aws.producer
  name     = "${substr(local.name, 0, 55)}-producer"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "redshift.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.producer.account_id } }
    }]
  })
}

resource "aws_iam_role" "consumer" {
  provider = aws.consumer
  name     = "${substr(local.name, 0, 55)}-consumer"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"] }
      Action    = ["sts:AssumeRole", "sts:SetContext"]
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.consumer.account_id } }
    }]
  })
}

resource "aws_iam_role_policy" "producer_fixture" {
  provider = aws.producer
  name     = "fixture"
  role     = aws_iam_role.producer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetBucketLocation", "s3:ListBucket"]
        Resource = module.fixture_bucket.s3_bucket_arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "${module.fixture_bucket.s3_bucket_arn}/data/*"
      },
      {
        Effect = "Allow"
        Action = ["glue:GetDatabase", "glue:GetDatabases", "glue:GetTable", "glue:GetTables", "glue:GetPartition", "glue:GetPartitions", "glue:BatchGetPartition"]
        Resource = [
          local.glue_catalog_arn,
          aws_glue_catalog_database.fixture.arn,
          local.glue_table_arn,
        ]
      },
    ]
  })
}

resource "aws_iam_role_policy" "consumer_fixture" {
  provider = aws.consumer
  name     = "fixture"
  role     = aws_iam_role.consumer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["s3:GetBucketLocation", "s3:ListBucket"]
        Resource = module.fixture_bucket.s3_bucket_arn
      },
      {
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = ["${module.fixture_bucket.s3_bucket_arn}/data/*", "${module.fixture_bucket.s3_bucket_arn}/unload/*"]
      },
      {
        Effect   = "Allow"
        Action   = ["s3:PutObject", "s3:AbortMultipartUpload"]
        Resource = "${module.fixture_bucket.s3_bucket_arn}/unload/*"
      },
      {
        Effect = "Allow"
        Action = ["glue:GetDatabase", "glue:GetDatabases", "glue:GetTable", "glue:GetTables", "glue:GetPartition", "glue:GetPartitions", "glue:BatchGetPartition"]
        Resource = [
          local.glue_catalog_arn,
          aws_glue_catalog_database.fixture.arn,
          local.glue_table_arn,
        ]
      },
    ]
  })
}
