terraform {
  required_version = ">= 1.14"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.67"
    }
    random = {
      source  = "hashicorp/random"
      version = ">= 3.7"
    }
    redshift = {
      source  = "netcheck-de/redshift"
      version = "~> 0.2"
    }
  }
}
