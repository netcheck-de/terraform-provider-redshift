# Short-lived test credential uses Secrets Manager's AWS-managed encryption key.
# trivy:ignore:AWS-0098
module "reader_secret" {
  source  = "terraform-aws-modules/secrets-manager/aws"
  version = "2.1.0"
  providers = {
    aws    = aws.consumer
    random = random.secrets
  }

  name                    = "${local.name}-reader"
  recovery_window_in_days = 0
  secret_string = jsonencode({
    username = redshift_user.reader.name
    password = random_password.reader.result
  })
}
