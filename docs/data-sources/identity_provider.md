---
subcategory: Identity and Access
page_title: redshift_identity_provider Data Source - terraform-provider-redshift
description: Looks up an existing AWSIDC SQL identity provider.
---

# redshift_identity_provider (Data Source)

Looks up an existing AWSIDC SQL identity provider.

## Example Usage

```hcl
data "redshift_identity_provider" "this" {
  name = "analytics-redshift-idc"
}
```

## Argument Reference

| Name   | Type             | Description                 |
|--------|------------------|-----------------------------|
| `name` | String, required | SQL identity provider name. |

## Attribute Reference

`id` (String, computed) is the observed identity provider's JSON identity, using the same warehouse, provider database,
and name keys as the paired resource.

| Name              | Type    | Description                              |
|-------------------|---------|------------------------------------------|
| `namespace`       | String  | Federated user and group-role prefix.    |
| `application_arn` | String  | Identity Center managed application ARN. |
| `iam_role_arn`    | String  | Integration IAM role ARN.                |
| `enabled`         | Boolean | Current enabled state.                   |

A missing provider or one with an incompatible type or malformed catalog parameters is an error.
