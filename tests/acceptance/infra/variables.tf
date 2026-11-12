variable "region" {
  description = "Commercial AWS region with Redshift Serverless capacity."
  type        = string
}

variable "name_prefix" {
  description = "Unique name for this disposable test run."
  type        = string
}

variable "db_name" {
  description = "Initial database for the acceptance tests."
  type        = string
  default     = "dev"
}

variable "vpc_cidr" {
  description = "CIDR block for the acceptance VPC."
  type        = string
  default     = "10.86.0.0/16"
}

variable "runner_cidr" {
  description = "Public IPv4 /32 of the test runner; broader ingress is rejected."
  type        = string
  validation {
    condition     = can(cidrnetmask(var.runner_cidr)) && can(regex("/32$", var.runner_cidr))
    error_message = "Supply a single IPv4 /32, never a public address range."
  }
}
