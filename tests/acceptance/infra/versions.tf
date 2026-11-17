terraform {
  required_version = ">= 1.16"

  backend "s3" {
    key          = "terraform-provider-redshift/live/acceptance.tfstate"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.8"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.73"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.9"
    }
    redshift = {
      source  = "netcheck-de/redshift"
      version = "= 0.5.0"
    }
  }
}
