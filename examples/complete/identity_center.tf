data "aws_ssoadmin_instances" "this" {
  provider = aws.consumer
  count    = local.sso_enabled ? 1 : 0
}

locals {
  # The instance ARNs and store IDs are independent sets, never parallel lists.
  # Discovery is safe only if the chosen instance exists and one store is returned.
  identity_store_id = !local.sso_enabled ? null : (
    var.identity_store_id != null ? var.identity_store_id : (
      contains(data.aws_ssoadmin_instances.this[0].arns, var.identity_center_instance_arn) &&
      length(data.aws_ssoadmin_instances.this[0].identity_store_ids) == 1
      ? one(data.aws_ssoadmin_instances.this[0].identity_store_ids) : null
    )
  )
}

resource "aws_iam_role_policy" "identity_center" {
  provider = aws.consumer
  count    = local.sso_enabled ? 1 : 0
  name     = "identity-center"
  role     = aws_iam_role.consumer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = "sso:DescribeInstance"
        Resource = var.identity_center_instance_arn
      },
      {
        Effect = "Allow"
        Action = "sso:DescribeApplication"
        # The managed application does not exist until this role is ready.
        # Organization instances can own the managed application in the management account.
        Resource = "arn:${data.aws_partition.consumer.partition}:sso::*:application/*"
      },
    ]
  })
}

resource "aws_redshift_idc_application" "this" {
  provider                      = aws.consumer
  count                         = local.sso_enabled ? 1 : 0
  redshift_idc_application_name = "${local.name}-consumer"
  idc_display_name              = "${local.name}-redshift"
  idc_instance_arn              = var.identity_center_instance_arn
  iam_role_arn                  = aws_iam_role.consumer.arn
  identity_namespace            = replace(local.name, "-", "_")

  lifecycle {
    precondition {
      condition     = contains(data.aws_ssoadmin_instances.this[0].arns, var.identity_center_instance_arn)
      error_message = "The supplied Identity Center instance must be visible in the consumer account and region."
    }

    precondition {
      condition     = local.identity_store_id != null
      error_message = "Supply identity_store_id when the selected instance cannot be matched to a unique discovered identity store."
    }
  }

  depends_on = [aws_iam_role_policy.identity_center]
}

locals {
  # Existing directory groups are only read and assigned; the example never changes their membership.
  sso_existing_groups = local.sso_enabled && var.identity_center_reader_group_name != null
  sso_create_groups   = local.sso_enabled && !local.sso_existing_groups
  # Exactly one source exists per group when SSO is enabled.
  sso_reader_group = one(concat(
    [for group in data.aws_identitystore_group.readers : { id = group.group_id, name = group.display_name }],
    [for group in aws_identitystore_group.readers : { id = group.group_id, name = group.display_name }],
  ))
  sso_operator_group = one(concat(
    [for group in data.aws_identitystore_group.operators : { id = group.group_id, name = group.display_name }],
    [for group in aws_identitystore_group.operators : { id = group.group_id, name = group.display_name }],
  ))
}

data "aws_identitystore_group" "readers" {
  provider          = aws.consumer
  count             = local.sso_existing_groups ? 1 : 0
  identity_store_id = local.identity_store_id
  alternate_identifier {
    unique_attribute {
      attribute_path  = "DisplayName"
      attribute_value = var.identity_center_reader_group_name
    }
  }
}

data "aws_identitystore_group" "operators" {
  provider          = aws.consumer
  count             = local.sso_existing_groups ? 1 : 0
  identity_store_id = local.identity_store_id
  alternate_identifier {
    unique_attribute {
      attribute_path  = "DisplayName"
      attribute_value = var.identity_center_operator_group_name
    }
  }
}

resource "aws_identitystore_group" "readers" {
  provider          = aws.consumer
  count             = local.sso_create_groups ? 1 : 0
  identity_store_id = local.identity_store_id
  display_name      = "${substr(local.name, 0, 55)}-readers"
  description       = "Readers of the complete Redshift example"

  lifecycle {
    precondition {
      condition     = local.identity_store_id != null
      error_message = "A unique Identity Center store or an explicit identity_store_id is required."
    }
  }

  depends_on = [aws_redshift_idc_application.this]
}

resource "aws_identitystore_group" "operators" {
  provider          = aws.consumer
  count             = local.sso_create_groups ? 1 : 0
  identity_store_id = local.identity_store_id
  display_name      = "${substr(local.name, 0, 54)}-operators"
  description       = "Operators of the complete Redshift example"

  lifecycle {
    precondition {
      condition     = local.identity_store_id != null
      error_message = "A unique Identity Center store or an explicit identity_store_id is required."
    }
  }

  depends_on = [aws_redshift_idc_application.this]
}

resource "aws_ssoadmin_application_assignment" "readers" {
  provider        = aws.consumer
  count           = local.sso_enabled ? 1 : 0
  application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn
  principal_id    = local.sso_reader_group.id
  principal_type  = "GROUP"
}

resource "aws_ssoadmin_application_assignment" "operators" {
  provider        = aws.consumer
  count           = local.sso_enabled ? 1 : 0
  application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn
  principal_id    = local.sso_operator_group.id
  principal_type  = "GROUP"
}

resource "aws_identitystore_group_membership" "reader" {
  provider          = aws.consumer
  count             = local.sso_create_groups && var.identity_center_test_user_id != null ? 1 : 0
  identity_store_id = local.identity_store_id
  group_id          = aws_identitystore_group.readers[0].group_id
  member_id         = var.identity_center_test_user_id
}

resource "redshift_identity_provider" "this" {
  provider        = redshift.consumer
  count           = local.sso_enabled ? 1 : 0
  name            = "example_redshift_idc"
  type            = "AWSIDC"
  namespace       = aws_redshift_idc_application.this[0].identity_namespace
  application_arn = aws_redshift_idc_application.this[0].idc_managed_application_arn
  iam_role_arn    = aws_iam_role.consumer.arn
  enabled         = true
  # The group roles below are managed explicitly, so Redshift must not create them at login.
  auto_create_roles = false
}

data "redshift_identity_provider" "this" {
  provider = redshift.consumer
  count    = local.sso_enabled ? 1 : 0
  name     = redshift_identity_provider.this[0].name
}

resource "redshift_role" "sso_readers" {
  provider = redshift.consumer
  count    = local.sso_enabled ? 1 : 0
  name     = "${redshift_identity_provider.this[0].namespace}:${local.sso_reader_group.name}"
}

resource "redshift_role" "sso_operators" {
  provider = redshift.consumer
  count    = local.sso_enabled ? 1 : 0
  name     = "${redshift_identity_provider.this[0].namespace}:${local.sso_operator_group.name}"
}

resource "redshift_role_grant" "sso_readers" {
  provider = redshift.consumer
  count    = local.sso_enabled ? 1 : 0
  role     = redshift_role.readers.name
  to_role  = redshift_role.sso_readers[0].name
}

resource "redshift_role_grant" "sso_operators" {
  provider = redshift.consumer
  count    = local.sso_enabled ? 1 : 0
  role     = redshift_role.operators.name
  to_role  = redshift_role.sso_operators[0].name
}
