resource "redshift_external_model" "summarizer" {
  database      = "analytics"
  schema        = "ml"
  name          = "summarizer"
  function      = "f_summarize"
  iam_role      = aws_iam_role.redshift_bedrock.arn
  model_id      = "anthropic.claude-3-haiku-20240307-v1:0"
  prompt        = "Summarize the following support ticket in one sentence:"
  request_type  = "UNIFIED"
  response_type = "VARCHAR"
}

# Bedrock calls are granted on the model; its inference function cannot be granted directly.
resource "redshift_object_grant" "summarizer" {
  database_name = redshift_external_model.summarizer.database
  schema_name   = redshift_external_model.summarizer.schema
  object_name   = redshift_external_model.summarizer.name
  object_type   = "MODEL"
  grantee_type  = "ROLE"
  grantee       = "support_analysts"
  privileges    = ["EXECUTE"]
}
