---
subcategory: Configuration
page_title: Connection methods and authentication
description: Configure Serverless/provisioned Data API or direct TLS password/IAM connections and preserve warehouse identity.
---

# Connection methods and authentication

Configure exactly one top-level selector: `workgroup_name`, `cluster_identifier`, or `direct_connection`. `database`
remains the required existing local administration database. Resource-specific database arguments route operations
within the selected warehouse. All resources and data sources share the same transport-neutral SQL implementation.

`direct_connection` is used because the Plugin Framework reserves `connection` as a root block name.

## Serverless Data API

```hcl
provider "redshift" {
  region         = "eu-central-1"
  profile        = "warehouse-admin"
  database       = "master"
  workgroup_name = "analytics"
}
```

IAM authentication uses the caller's SQL identity through temporary credentials. `workgroup_name` accepts a name or ARN.
No direct endpoint connectivity is required. `secret_arn` optionally selects Secrets Manager authentication; `db_user`
is not accepted for Serverless.

## Provisioned Data API

```hcl
provider "redshift" {
  region             = "eu-central-1"
  profile            = "warehouse-admin"
  database           = "analytics"
  cluster_identifier = "analytics-cluster"
  db_user            = "terraform_admin"
}
```

Require exactly one of `db_user` or `secret_arn`. `db_user` selects an existing database identity through temporary
cluster credentials. `secret_arn` selects a Secrets Manager secret instead. These top-level authentication arguments
cannot be used with `direct_connection`. Cluster identifiers are names, not cluster ARNs.

## Direct password authentication

```hcl
variable "redshift_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

provider "redshift" {
  database = "analytics"

  direct_connection {
    host     = "redshift.internal.example.com"
    port     = 5439
    username = "terraform_admin"
    password = var.redshift_password
  }
}
```

Require nonempty `host`, `username`, and `password`. `port` defaults to 5439 and must be between 1 and 65535. This works
with both warehouse types and needs no AWS credentials or region. Passwords are sensitive provider configuration and are
never included in resource state or import IDs. Ephemeral Terraform input is supported.

Shared `data.redshift_database` lookups additionally resolve `datashare_arn` through the AWS Redshift control plane.
Those lookups need AWS credentials, a consumer-region configuration, and `redshift:DescribeDataShares`,
including in direct password mode. Ordinary SQL operations and local database lookups do not need this discovery call.

## Direct IAM authentication

### Serverless

```hcl
provider "redshift" {
  region   = "eu-central-1"
  profile  = "warehouse-admin"
  database = "master"

  direct_connection {
    iam {
      workgroup_name = "analytics"
    }
  }
}
```

Endpoint discovery uses `GetWorkgroup`; workgroup ARNs additionally require `ListWorkgroups` to resolve their UUID to a
name. `GetCredentials` supplies the SQL username and temporary password separately for each requested database.

### Provisioned

```hcl
provider "redshift" {
  region   = "eu-central-1"
  profile  = "warehouse-admin"
  database = "analytics"

  direct_connection {
    iam {
      cluster_identifier = "analytics-cluster"
      db_user            = "terraform_admin"
    }
  }
}
```

Endpoint discovery uses `DescribeClusters`. `GetClusterCredentials` authenticates the existing `db_user` with
`AutoCreate = false` and no session group assignment. IAM permissions and SQL privileges are separate requirements.
Unknown warehouse/endpoint values are allowed during planning; SQL waits until the binding and authentication are known.

## TLS and routing

Direct connections use TLS 1.2 or later. `direct_connection.sslmode` selects verification and defaults to `verify-full`
(certificate chain and hostname). `verify-ca` checks only the chain, `require` encrypts without verifying the server, and
`disable` sends credentials and data in plaintext; use the weaker modes only on trusted networks. Fallback modes such as
`prefer` are not supported, so a connection never silently downgrades.

On macOS, Go uses the platform verifier, which rejects Redshift Serverless certificates that lack Certificate
Transparency timestamps, in both `verify-full` and `verify-ca`. Adding the
[Amazon Trust Services root](https://www.amazontrust.com/repository/) through `ca_cert_file` keeps full verification;
`require` also connects, without verification. Linux is not affected.

Discovery prefers an AWS-configured custom domain because its certificate can differ from the default AWS
endpoint certificate. An optional `direct_connection.host` overrides discovery routing and the TLS server name; `port`
optionally overrides the discovered port. These overrides do not change an IAM warehouse's import identity.

`direct_connection.ca_cert_file` adds a PEM CA bundle to system trust for custom certificate authorities in the
verifying modes. The runner
needs network access to the endpoint; private warehouses usually require VPC connectivity or VPN access.

Each statement opens and deterministically closes its own database-specific connection in autocommit mode. IAM
credentials are cached only in process memory and refreshed before expiration. This avoids an unbounded connection pool
without a provider shutdown hook. Parameter binding uses uncached PostgreSQL wire-protocol execution compatible with
Redshift, preserving quotes, comments, dollar-quoted bodies, and casts. Ambiguous network failures do not trigger
mutation retries. `direct_connection.connect_timeout` (default `30s`) bounds opening each connection, while
`query_timeout` bounds the whole statement, including IAM credential lookups; see "Timeouts and retries" on the
provider page.

## Imports and transport switching

All JSON import IDs contain an administration `database`, exactly one warehouse key, and resource-specific fields:

| Method                          | Warehouse key        | Example                                |
|---------------------------------|----------------------|----------------------------------------|
| Serverless Data API/direct IAM  | `workgroup_name`     | `"analytics"` or its configured ARN    |
| Provisioned Data API/direct IAM | `cluster_identifier` | `"analytics-cluster"`                  |
| Direct password                 | `endpoint`           | `"redshift.internal.example.com:5439"` |

Existing Serverless IDs remain unchanged. Switching Data API to direct IAM for the same selector spelling preserves
resource identity and needs no import or replacement. Name and ARN spellings are deliberately not treated as equivalent;
changing the spelling requires explicit re-importing. Password endpoint identities normalize DNS case/trailing dots and
include the port. Switching between AWS identity and password endpoint identity requires explicit re-importing.

```sh
terraform import redshift_role.readers \
  '{"cluster_identifier":"analytics-cluster","database":"analytics","name":"readers"}'

terraform import redshift_role.readers \
  '{"endpoint":"redshift.internal.example.com:5439","database":"analytics","name":"readers"}'
```

The same identity works in an `import` block (Terraform 1.5 and later):

```terraform
import {
  to = redshift_role.readers
  id = jsonencode({
    cluster_identifier = "analytics-cluster"
    database           = "analytics"
    name               = "readers"
  })
}
```

Do not destroy/recreate SQL objects merely to switch bindings. Re-import the same objects under the intended
configuration and inspect the next plan. Read-only data sources never need imports.
