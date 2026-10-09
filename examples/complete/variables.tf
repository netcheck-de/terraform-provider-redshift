variable "region" {
  description = "AWS Region supporting RA3, Redshift Serverless, and at least three availability zones."
  type        = string
  default     = "eu-central-1"
}

variable "profile" {
  description = "Optional AWS profile for both accounts; null uses the standard credential chain."
  type        = string
  default     = null
}

variable "producer_profile" {
  description = "Optional producer profile override, including for a cross-account sharing scenario."
  type        = string
  default     = null
}

variable "consumer_profile" {
  description = "Optional consumer profile override; default credentials create both warehouses in one account."
  type        = string
  default     = null
}

variable "name_prefix" {
  description = "AWS resource prefix; a random suffix isolates this disposable environment."
  type        = string
  default     = "redshift-oss-e2e"

  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,29}$", var.name_prefix))
    error_message = "Use 3–30 lowercase letters, digits, or hyphens, starting with a letter."
  }
}

variable "producer_vpc_cidr" {
  description = "Producer VPC IPv4 CIDR; three subnets are allocated with eight additional prefix bits."
  type        = string
  default     = "10.42.0.0/16"

  validation {
    condition     = can(cidrnetmask(var.producer_vpc_cidr)) && can(cidrsubnet(var.producer_vpc_cidr, 8, 2))
    error_message = "Supply an IPv4 CIDR large enough to create three subnets with eight additional prefix bits."
  }
}

variable "consumer_vpc_cidr" {
  description = "Consumer VPC IPv4 CIDR; no peering is needed for Data API or datasharing."
  type        = string
  default     = "10.43.0.0/16"

  validation {
    condition     = can(cidrnetmask(var.consumer_vpc_cidr)) && can(cidrsubnet(var.consumer_vpc_cidr, 8, 2))
    error_message = "Supply an IPv4 CIDR large enough to create three subnets with eight additional prefix bits."
  }
}

variable "cluster_node_type" {
  description = "Single-node RA3 producer type; regional availability and quotas still apply."
  type        = string
  default     = "ra3.xlplus"

  validation {
    condition     = contains(["ra3.large", "ra3.xlplus"], var.cluster_node_type)
    error_message = "The single-node example supports ra3.large or ra3.xlplus."
  }
}

variable "serverless_capacity" {
  description = "Fixed Serverless base/max RPUs; 8 is supported in the default region."
  type        = number
  default     = 8

  validation {
    condition     = var.serverless_capacity >= 8 && var.serverless_capacity <= 512 && var.serverless_capacity % 8 == 0
    error_message = "Use a supported capacity from 8–512 RPUs in multiples of 8."
  }
}

variable "admin_username" {
  description = "Warehouse administrator username; AWS manages both administrator passwords."
  type        = string
  default     = "tfadmin"

  validation {
    condition     = can(regex("^[a-z][a-z0-9_]{2,31}$", var.admin_username))
    error_message = "Use 3–32 lowercase letters, digits, or underscores, starting with a letter."
  }
}

variable "admin_database" {
  description = "AWS-created administration database, separate from SQL-owned example databases."
  type        = string
  default     = "dev"

  validation {
    condition     = can(regex("^[a-z][a-z0-9_]{0,62}$", var.admin_database)) && !contains(["example_producer", "example_local", "example_shared"], var.admin_database)
    error_message = "Use a SQL identifier distinct from the example's managed databases."
  }
}

variable "enable_assumerole_grant" {
  description = "Initialize identity-specific ASSUMEROLE access on the disposable consumer and demonstrate COPY/UNLOAD grants."
  type        = bool
  default     = true
}

variable "identity_center_instance_arn" {
  description = "Existing IAM Identity Center instance ARN in the consumer region; null skips all SSO resources."
  type        = string
  default     = null

  validation {
    condition     = var.identity_center_instance_arn == null ? true : can(regex("^arn:[a-z0-9-]+:sso:::instance/ssoins-[a-zA-Z0-9-]+$", var.identity_center_instance_arn))
    error_message = "Supply an Identity Center instance ARN, not a Redshift application ARN, or leave it null."
  }
}

variable "identity_store_id" {
  description = "Optional store ID override when discovery cannot identify a unique store for the chosen SSO instance."
  type        = string
  default     = null
}

variable "identity_center_reader_group_name" {
  description = "Existing Identity Center group display name to map to the reader role; null creates an example group."
  type        = string
  default     = null

  validation {
    condition     = (var.identity_center_reader_group_name == null) == (var.identity_center_operator_group_name == null)
    error_message = "Set both existing Identity Center group names or neither."
  }
}

variable "identity_center_operator_group_name" {
  description = "Existing Identity Center group display name to map to the operator role; null creates an example group."
  type        = string
  default     = null
}

variable "identity_center_test_user_id" {
  description = "Optional existing Identity Center user to join the created reader group for interactive SSO testing."
  type        = string
  default     = null
}

variable "allow_public_sql" {
  description = "Opt in to internet-routable SQL endpoints for direct connection checks from the allowed client CIDRs."
  type        = bool
  default     = false

  validation {
    condition     = !var.allow_public_sql || length(var.public_sql_cidrs) > 0
    error_message = "Public SQL endpoints require explicit public_sql_cidrs."
  }
}

variable "public_sql_cidrs" {
  description = "Explicit IPv4 client CIDRs allowed to connect on port 5439 when public SQL is enabled."
  type        = list(string)
  default     = []

  validation {
    condition     = alltrue([for cidr in var.public_sql_cidrs : can(cidrnetmask(cidr)) && cidr != "0.0.0.0/0"])
    error_message = "Supply valid, restricted IPv4 CIDRs; unrestricted internet ingress is not supported."
  }
}

variable "connection_checks" {
  description = "Optional read-only connection probes; direct modes require endpoint connectivity and password probes require ephemeral inputs."
  type        = set(string)
  default     = []

  validation {
    condition = length(setsubtract(var.connection_checks, toset([
      "producer_data_api_iam", "consumer_data_api_iam",
      "producer_direct_iam", "consumer_direct_iam",
      "producer_direct_password", "consumer_direct_password",
    ]))) == 0
    error_message = "Choose one or more of the documented producer/consumer Data API IAM, direct IAM, or direct password probes."
  }
}

variable "direct_sslmode" {
  description = "TLS mode for direct probes. On macOS, Go's platform verifier rejects Serverless certificates without Certificate Transparency timestamps; use require there."
  type        = string
  default     = "verify-full"

  validation {
    condition     = contains(["verify-full", "verify-ca", "require", "disable"], var.direct_sslmode)
    error_message = "Choose verify-full, verify-ca, require, or disable."
  }
}

variable "producer_password" {
  description = "Ephemeral administrator password for an optional direct producer probe; obtain it from the managed secret outside Terraform state."
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = null

  validation {
    condition     = !contains(var.connection_checks, "producer_direct_password") || var.producer_password != null
    error_message = "The producer_direct_password probe requires TF_VAR_producer_password."
  }
}

variable "consumer_password" {
  description = "Ephemeral administrator password for an optional direct consumer probe."
  type        = string
  sensitive   = true
  ephemeral   = true
  default     = null

  validation {
    condition     = !contains(var.connection_checks, "consumer_direct_password") || var.consumer_password != null
    error_message = "The consumer_direct_password probe requires TF_VAR_consumer_password."
  }
}
