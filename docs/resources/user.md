---
subcategory: Identity and Access
page_title: redshift_user Resource - terraform-provider-redshift
description: Manages SQL users and write-only password rotation.
---

# redshift_user (Resource)

Manages a password-authenticated database user. Role memberships and object privileges are independent resources. See
AWS [CREATE USER](https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_USER.html).

## Example Usage

```hcl
resource "redshift_user" "grafana" {
  name                = "grafana"
  password_wo         = random_password.grafana.result
  password_wo_version = 0
}

resource "redshift_role_grant" "grafana_monitor" {
  role    = "sys:monitor"
  to_user = redshift_user.grafana.name

}
```

## Argument Reference

| Name                  | Type                        | Description                                                                 |
|-----------------------|-----------------------------|-----------------------------------------------------------------------------|
| `name`                | String, required            | User name; changing it replaces the resource.                               |
| `password_wo`         | String, optional/write-only | Required on creation or rotation; not stored in this resource's plan/state. |
| `password_wo_version` | Number, optional/computed   | Version trigger, default `0`. Increment to rotate the password.             |
| `superuser`           | Boolean, optional/computed  | `CREATEUSER`, default `false`; updated in place.                            |
| `create_database`     | Boolean, optional/computed  | `CREATEDB`, default `false`; updated in place.                              |

## Attribute Reference

| Name | Type   | Description                                        |
|------|--------|----------------------------------------------------|
| `id` | String | Stable JSON import identity; contains no password. |

## Lifecycle and Ownership

Reads discover non-secret properties from `pg_user`; password values cannot be refreshed or compared. The provider
changes the password only on creation or a version change. A missing password during rotation is an error. Deletion
issues `DROP USER` without `CASCADE`; dependencies, including role grants, must be removed first.

Refresh updates `superuser` and `create_database` from the catalog. `password_wo_version` retains the configured rotation
revision rather than a catalog value.

Write-only values are absent from **this resource's** state. The password source (for example, `random_password`) may
still keep a value in its own Terraform state, and the Data API executes a SQL statement containing the password.
Restrict Terraform state, provider logs, and Data API query-history access accordingly.

## Import

```sh
terraform import redshift_user.grafana \
  '{"workgroup_name":"warehouse","database":"admin","name":"grafana"}'
```

Import sets `password_wo_version` to `0` and does **not** rotate the existing password. Match the configured version and
non-secret flags to the imported state for a no-change plan. Increase the version later to rotate the password.
