# AWS-only fixture: the acceptance tests create and drop the SQL external function.
data "archive_file" "extfunctions_upper" {
  type        = "zip"
  output_path = "${path.module}/.terraform/extfunctions_upper.zip"

  source {
    filename = "index.py"
    # TestAccExternalFunctionLifecycle declares (VARCHAR, INTEGER). Redshift sends
    # batches of rows; the second argument is accepted but does not affect uppercase.
    content = <<-PYTHON
      import json


      def handler(event, context):
          results = [None if row is None or row[0] is None else row[0].upper() for row in event["arguments"]]
          return json.dumps({"success": True, "num_records": len(results), "results": results})
    PYTHON
  }
}

resource "aws_iam_role" "lambda_execution" {
  name = "${substr(var.name_prefix, 0, 50)}-udf-exec"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda_execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_lambda_function" "extfunctions_upper" {
  function_name    = "${substr(var.name_prefix, 0, 50)}-upper"
  role             = aws_iam_role.lambda_execution.arn
  runtime          = "python3.12"
  handler          = "index.handler"
  filename         = data.archive_file.extfunctions_upper.output_path
  source_code_hash = data.archive_file.extfunctions_upper.output_base64sha256
}

# Associate this role with the namespace alongside the warehouse role. Export its
# ARN as REDSHIFT_ACC_LAMBDA_ROLE; the default warehouse role needs no Lambda access.
resource "aws_iam_role" "lambda_invocation" {
  name = "${substr(var.name_prefix, 0, 50)}-udf-invoke"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Principal = {
        Service = ["redshift.amazonaws.com", "redshift-serverless.amazonaws.com"]
      }
      Action = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "lambda_invocation" {
  name = "lambda-udf"
  role = aws_iam_role.lambda_invocation.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = aws_lambda_function.extfunctions_upper.arn
    }]
  })
}
