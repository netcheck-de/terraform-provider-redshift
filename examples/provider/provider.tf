terraform {
  required_providers {
    redshift = {
      source  = "netcheck-de/redshift"
      version = "0.2.0"
    }
  }
}

provider "redshift" {
  region         = "eu-central-1"
  profile        = "warehouse-admin"
  workgroup_name = "analytics"
  database       = "master"
}
