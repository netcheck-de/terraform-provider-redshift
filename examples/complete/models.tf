variable "bedrock_model_id" {
  description = "Amazon Bedrock model ID for the optional external model, such as amazon.nova-lite-v1:0; null skips the Bedrock resources. The model must be enabled in Amazon Bedrock for the consumer account and region."
  type        = string
  default     = null
}

locals {
  bedrock_enabled = var.bedrock_model_id != null
}

# The consumer namespace's default role invokes the Bedrock model on behalf of Redshift ML.
resource "aws_iam_role_policy" "consumer_bedrock" {
  provider = aws.consumer
  count    = local.bedrock_enabled ? 1 : 0
  name     = "bedrock-model"
  role     = aws_iam_role.consumer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "bedrock:InvokeModel"
      Resource = "arn:${data.aws_partition.consumer.partition}:bedrock:${var.region}::foundation-model/${var.bedrock_model_id}"
    }]
  })
}

# Redshift creates the inference function with the model and drops it with the model, so no redshift_function owns it.
resource "redshift_external_model" "summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database      = redshift_schema.local.database
  schema        = redshift_schema.local.name
  name          = "example_summarizer"
  function      = "f_example_summarize"
  iam_role      = "DEFAULT"
  model_id      = var.bedrock_model_id
  prompt        = "Summarize the following text in one sentence:"
  request_type  = "UNIFIED"
  response_type = "VARCHAR"
  # Creation does not invoke Bedrock, but the first query does.
  # Drop the model before its grantees: generated inference functions can retain principal ACL dependencies.
  depends_on = [
    aws_iam_role_policy.consumer_bedrock,
    redshift_user.model_delegate,
    redshift_group.readers,
    redshift_role.readers,
  ]
}

# The inference function cannot be granted on its own; readers may call it through EXECUTE on the model.
resource "redshift_object_grant" "readers_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_external_model.summarizer[0].database
  schema_name   = redshift_external_model.summarizer[0].schema
  object_name   = redshift_external_model.summarizer[0].name
  object_type   = "MODEL"
  grantee_type  = "ROLE"
  grantee       = redshift_role.readers.name
  privileges    = ["EXECUTE"]
}

# This separate IAM-only identity may delegate model access; the example never invokes the inference function.
resource "redshift_user" "model_delegate" {
  provider          = redshift.consumer
  count             = local.bedrock_enabled ? 1 : 0
  name              = "example_model_delegate"
  password_disabled = true
}

resource "redshift_object_grant" "model_delegate_schema" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_schema.local.database
  schema_name   = redshift_schema.local.name
  object_type   = "SCHEMA"
  grantee_type  = "USER"
  grantee       = redshift_user.model_delegate[0].name
  privileges    = ["USAGE"]
}

data "redshift_object_grant" "model_delegate_schema" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_object_grant.model_delegate_schema[0].database_name
  schema_name   = redshift_object_grant.model_delegate_schema[0].schema_name
  object_type   = "SCHEMA"
  grantee_type  = "USER"
  grantee       = redshift_object_grant.model_delegate_schema[0].grantee
}

resource "redshift_object_grant" "delegate_summarizer" {
  provider                = redshift.consumer
  count                   = local.bedrock_enabled ? 1 : 0
  database_name           = redshift_external_model.summarizer[0].database
  schema_name             = redshift_external_model.summarizer[0].schema
  object_name             = redshift_external_model.summarizer[0].name
  object_type             = "MODEL"
  grantee_type            = "USER"
  grantee                 = redshift_user.model_delegate[0].name
  privileges              = ["EXECUTE"]
  grant_option_privileges = ["EXECUTE"]
}

resource "redshift_object_grant" "group_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_external_model.summarizer[0].database
  schema_name   = redshift_external_model.summarizer[0].schema
  object_name   = redshift_external_model.summarizer[0].name
  object_type   = "MODEL"
  grantee_type  = "GROUP"
  grantee       = redshift_group.readers.name
  privileges    = ["EXECUTE"]
}

# External models have no implicit PUBLIC EXECUTE. Own the empty tuple so an explicit outside grant is revoked.
resource "redshift_object_grant" "public_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_external_model.summarizer[0].database
  schema_name   = redshift_external_model.summarizer[0].schema
  object_name   = redshift_external_model.summarizer[0].name
  object_type   = "MODEL"
  grantee_type  = "PUBLIC"
  grantee       = "public"
  privileges    = []
}

data "redshift_user" "model_delegate" {
  provider = redshift.consumer
  count    = local.bedrock_enabled ? 1 : 0
  name     = redshift_user.model_delegate[0].name
}

data "redshift_object_grant" "delegate_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_object_grant.delegate_summarizer[0].database_name
  schema_name   = redshift_object_grant.delegate_summarizer[0].schema_name
  object_name   = redshift_object_grant.delegate_summarizer[0].object_name
  object_type   = "MODEL"
  grantee_type  = "USER"
  grantee       = redshift_object_grant.delegate_summarizer[0].grantee
}

data "redshift_object_grant" "group_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_object_grant.group_summarizer[0].database_name
  schema_name   = redshift_object_grant.group_summarizer[0].schema_name
  object_name   = redshift_object_grant.group_summarizer[0].object_name
  object_type   = "MODEL"
  grantee_type  = "GROUP"
  grantee       = redshift_object_grant.group_summarizer[0].grantee
}

data "redshift_object_grant" "public_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_object_grant.public_summarizer[0].database_name
  schema_name   = redshift_object_grant.public_summarizer[0].schema_name
  object_name   = redshift_object_grant.public_summarizer[0].object_name
  object_type   = "MODEL"
  grantee_type  = "PUBLIC"
  grantee       = redshift_object_grant.public_summarizer[0].grantee
}

data "redshift_external_model" "summarizer" {
  provider = redshift.consumer
  count    = local.bedrock_enabled ? 1 : 0
  database = redshift_external_model.summarizer[0].database
  schema   = redshift_external_model.summarizer[0].schema
  name     = redshift_external_model.summarizer[0].name
}

data "redshift_object_grant" "readers_summarizer" {
  provider      = redshift.consumer
  count         = local.bedrock_enabled ? 1 : 0
  database_name = redshift_object_grant.readers_summarizer[0].database_name
  schema_name   = redshift_object_grant.readers_summarizer[0].schema_name
  object_name   = redshift_object_grant.readers_summarizer[0].object_name
  object_type   = "MODEL"
  grantee_type  = "ROLE"
  grantee       = redshift_object_grant.readers_summarizer[0].grantee
}

# SHOW MODEL ALL lists trained models too; the listing works with or without the Bedrock model.
data "redshift_models" "local" {
  provider   = redshift.consumer
  database   = redshift_schema.local.database
  schema     = redshift_schema.local.name
  depends_on = [redshift_external_model.summarizer]
}
