---
subcategory: Identity and Access
page_title: redshift_identity_provider Resource - terraform-provider-redshift
description: Manages the SQL side of an AWS Identity Center integration.
---

# redshift_identity_provider (Resource)

Manages an AWSIDC **SQL identity provider** in a Serverless warehouse. Create the managed Identity Center application,
integration IAM role, role attachment to the namespace, and group assignments with the AWS provider. See AWS
[CREATE IDENTITY PROVIDER](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_IDENTITY_PROVIDER.html).

## Example Usage

```hcl
resource "redshift_identity_provider" "this" {
  name            = "analytics-redshift-idc"
  namespace       = "ncidc"
  application_arn = aws_redshift_idc_application.this.idc_managed_application_arn
  iam_role_arn    = aws_iam_role.idc.arn
}
```

## Argument Reference

| Name              | Type                       | Description                                                                 |
|-------------------|----------------------------|-----------------------------------------------------------------------------|
| `name`            | String, required           | SQL provider name; changing it replaces the resource.                       |
| `namespace`       | String, required           | Federated user and role prefix; changing it replaces the resource.          |
| `application_arn` | String, required           | Identity Center managed application ARN; changing it replaces the resource. |
| `iam_role_arn`    | String, required           | Attached integration IAM role; updated in place.                            |
| `enabled`         | Boolean, optional/computed | Whether the provider is enabled, default `true`; updated in place.          |

## Attribute Reference

| Name | Type   | Description                  |
|------|--------|------------------------------|
| `id` | String | Stable JSON import identity. |

## Lifecycle and Ownership

Creation requires a Redshift database superuser. A read checks type `awsidc`, application binding, namespace, IAM role,
and enabled status. Deletion refuses to drop the provider while namespace-prefixed federated users remain. Manage role
grants and memberships separately; use resource dependencies so they are removed first.

## Import

```sh
terraform import redshift_identity_provider.this \
  '{"workgroup_name":"warehouse","database":"admin","name":"analytics-redshift-idc"}'
```

Refresh discovers the namespace, application ARN, IAM role ARN, and enabled state. Match configuration to that identity
before applying a plan.
