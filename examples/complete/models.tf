variable "bedrock_model_id" {
  description = "Amazon Bedrock model ID for the optional external model, such as anthropic.claude-3-haiku-20240307-v1:0; null skips the Bedrock resources. The model must be enabled in Amazon Bedrock for the consumer account and region."
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
  depends_on = [aws_iam_role_policy.consumer_bedrock]
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
