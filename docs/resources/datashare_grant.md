---
subcategory: Data Sharing
page_title: redshift_datashare_grant Resource - terraform-provider-redshift
description: Manages SQL datashare usage granted to a consumer account or namespace.
---

# redshift_datashare_grant (Resource)

Grants SQL `USAGE` on a producer datashare to **one consumer AWS account or Redshift namespace UUID**. Specify exactly
one of `account_id` and `namespace_id`. Cross-account sharing also requires AWS
authorization in the producer account and association in the consumer account. See AWS
[GRANT USAGE ON DATASHARE](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-datashare-syntax).

## Example Usage

```hcl
resource "redshift_datashare_grant" "consumer" {
  database   = redshift_datashare.producer.database
  datashare  = redshift_datashare.producer.name
  account_id = var.consumer_account_id
}

resource "aws_redshift_data_share_authorization" "consumer" {
  provider            = aws.producer
  data_share_arn      = local.datashare_arn
  consumer_identifier = var.consumer_account_id

  depends_on = [redshift_datashare_grant.consumer]
}

resource "aws_redshift_data_share_consumer_association" "consumer" {
  provider       = aws.consumer
  data_share_arn = aws_redshift_data_share_authorization.consumer.data_share_arn
  consumer_arn   = var.consumer_namespace_arn
}
```

For a namespace in the same AWS account, grant usage directly to its UUID:

```hcl
resource "redshift_datashare_grant" "namespace" {
  database     = redshift_datashare.producer.database
  datashare    = redshift_datashare.producer.name
  namespace_id = var.consumer_namespace_id
}
```

The namespace UUID may be unknown during planning, for example when a consumer warehouse is being created in the same
apply. It must resolve before SQL execution. Use the UUID, not the namespace ARN.

## Argument Reference

| Name           | Type             | Description                                                                                               |
|----------------|------------------|-----------------------------------------------------------------------------------------------------------|
| `database`     | String, required | Local producer database; changing it replaces the grant.                                                  |
| `datashare`    | String, required | Producer datashare name; changing it replaces the grant.                                                  |
| `account_id`   | String, optional | 12-digit consumer AWS account ID; mutually exclusive with `namespace_id`. Changing it replaces the grant. |
| `namespace_id` | String, optional | Consumer Redshift namespace UUID; mutually exclusive with `account_id`. Changing it replaces the grant.   |

Exactly one consumer selector is required.

## Attribute Reference

| Name | Type   | Description                                                                                                              |
|------|--------|--------------------------------------------------------------------------------------------------------------------------|
| `id` | String | Stable JSON identity including producer warehouse, database, datashare, and the selected `account_id` or `namespace_id`. |

## Lifecycle and Ownership

Uses `GRANT/REVOKE USAGE ON DATASHARE ... TO/FROM ACCOUNT 'account-id'` or `... TO/FROM NAMESPACE 'namespace-uuid'`.
Reads [SVV_DATASHARE_CONSUMERS](https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_DATASHARE_CONSUMERS.html), filtering
by share name and consumer account with null/empty `consumer_namespace` for account grants, or by share name and exact
`consumer_namespace` for namespace grants. This view has no consumer-type column; the namespace distinguishes the
grant scope. Account and namespace grants are independent, including when the namespace belongs to the same account. This
resource cannot authorize or associate a cross-account share: use the two AWS resources above. The
[complete example](https://github.com/netcheck-de/terraform-provider-redshift/tree/main/examples/complete) shows the
full producer-to-consumer sequence.

## Import

```sh
terraform import redshift_datashare_grant.consumer \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","account_id":"123456789012"}'
```

Namespace grants use `namespace_id` instead of `account_id` in the JSON identity:

```sh
terraform import redshift_datashare_grant.namespace \
  '{"workgroup_name":"warehouse","database":"warehouse","datashare":"analytics","namespace_id":"12345678-1234-1234-1234-123456789abc"}'
```

Existing account JSON identities and state retain the `account_id` key. Imports require exactly one valid consumer
selector and one warehouse binding (`workgroup_name`, `cluster_identifier`, or `endpoint`).
