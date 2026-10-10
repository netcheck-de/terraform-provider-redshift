# Scalar Lambda UDF: the AWS provider owns the Lambda function and the IAM permissions, Redshift owns the SQL function.
data "archive_file" "extfunctions_upper" {
  type        = "zip"
  output_path = "${path.module}/.terraform/extfunctions_upper.zip"

  source {
    filename = "index.py"
    # Redshift batches rows into "arguments" and expects one result per row, in order; a whole row may be null.
    content = <<-PYTHON
      import json


      def handler(event, context):
          results = [None if row is None or row[0] is None else row[0].upper() for row in event["arguments"]]
          return json.dumps({"success": True, "num_records": len(results), "results": results})
    PYTHON
  }
}

resource "aws_iam_role" "extfunctions_lambda" {
  provider = aws.consumer
  name     = "${substr(local.name, 0, 50)}-udf-exec"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "extfunctions_lambda_logs" {
  provider   = aws.consumer
  role       = aws_iam_role.extfunctions_lambda.name
  policy_arn = "arn:${data.aws_partition.consumer.partition}:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_lambda_function" "extfunctions_upper" {
  provider         = aws.consumer
  function_name    = "${substr(local.name, 0, 50)}-upper"
  role             = aws_iam_role.extfunctions_lambda.arn
  runtime          = "python3.12"
  handler          = "index.handler"
  filename         = data.archive_file.extfunctions_upper.output_path
  source_code_hash = data.archive_file.extfunctions_upper.output_base64sha256
  depends_on       = [aws_iam_role_policy_attachment.extfunctions_lambda_logs]
}

# The consumer namespace's associated role invokes the function on behalf of Redshift.
resource "aws_iam_role_policy" "consumer_lambda_udf" {
  provider = aws.consumer
  name     = "lambda-udf"
  role     = aws_iam_role.consumer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.extfunctions_upper.arn
    }]
  })
}

resource "redshift_external_function" "upper" {
  provider        = redshift.consumer
  database        = redshift_schema.local.database
  schema          = redshift_schema.local.name
  name            = "f_example_upper"
  arguments       = ["varchar"]
  return_type     = "varchar"
  volatility      = "STABLE"
  lambda_function = aws_lambda_function.extfunctions_upper.function_name
  iam_role        = aws_iam_role.consumer.arn
  retry_timeout   = 3000
  max_batch_rows  = 500
  # Creation does not invoke Lambda, but the first query does.
  depends_on = [aws_iam_role_policy.consumer_lambda_udf]
}

data "redshift_functions" "example" {
  provider = redshift.consumer
  database = redshift_external_function.upper.database
  schema   = redshift_external_function.upper.schema
  name     = redshift_external_function.upper.name
}

data "redshift_procedures" "example" {
  provider = redshift.consumer
  database = redshift_schema.local.database
  schema   = redshift_schema.local.name
}

data "redshift_routine_parameters" "upper" {
  provider     = redshift.consumer
  database     = redshift_external_function.upper.database
  schema       = redshift_external_function.upper.schema
  routine_name = redshift_external_function.upper.name
  routine_type = "FUNCTION"
}
