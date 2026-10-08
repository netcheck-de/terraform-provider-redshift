---
subcategory: Data Sharing
page_title: redshift_datashare_grant Data Source - terraform-provider-redshift
description: Checks explicit SQL share usage granted to a consumer account or namespace.
---

# redshift_datashare_grant (Data Source)

Checks SQL account- or namespace-level datashare usage. Specify exactly one of `account_id` and `namespace_id`.
This does not verify AWS authorization or consumer association. See AWS
[GRANT](https://docs.aws.amazon.com/redshift/latest/dg/r_GRANT.html#grant-datashare-syntax).

## Example Usage

```hcl
data "redshift_datashare_grant" "consumer" {
  database   = "analytics"
  datashare  = "reports"
  account_id = "123456789012"
}

data "redshift_datashare_grant" "namespace" {
  database     = "analytics"
  datashare    = "reports"
  namespace_id = "12345678-1234-1234-1234-123456789abc"
}
```

## Argument Reference

| Name           | Type             | Description                                                                |
|----------------|------------------|----------------------------------------------------------------------------|
| `database`     | String, required | Existing producer database.                                                |
| `datashare`    | String, required | Outbound share name.                                                       |
| `account_id`   | String, optional | 12-digit consumer AWS account ID; mutually exclusive with `namespace_id`.  |
| `namespace_id` | String, optional | Consumer namespace UUID, not an ARN; mutually exclusive with `account_id`. |

Exactly one consumer selector is required. A namespace UUID may be unknown during planning; the lookup is deferred
until its value resolves.

## Attribute Reference

`id` (String, computed) is the relationship's JSON identity, using the same warehouse, producer database, datashare, and
selected `account_id` or `namespace_id` key as the paired resource. Existing account identities retain their format.
It is null when `exists` is false.

| Name     | Type    | Description                                                                |
|----------|---------|----------------------------------------------------------------------------|
| `exists` | Boolean | Whether the selected explicit account or namespace SQL usage grant exists. |

Absent SQL grants return false. Missing databases and catalog errors raise errors. No SQL grants or AWS resources
change.

Reads [SVV_DATASHARE_CONSUMERS](https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_DATASHARE_CONSUMERS.html).
Account lookups match the share and account with an empty/null namespace. Namespace lookups match the share and exact
namespace UUID; an account-wide grant does not count as an explicit namespace grant. The catalog has no consumer-type
column. Only read-only SQL is executed.
