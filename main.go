// Package main serves the Redshift SQL Terraform provider.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/netcheck-de/terraform-provider-redshift/internal/provider"
)

// Generate the Terraform Registry documentation in docs/ from templates/, examples/, and the provider schema.
//go:generate go tool tfplugindocs generate --provider-name redshift

// version is supplied by the build linker and defaults to dev for unversioned runs.
var version = "dev"

// main starts the Terraform protocol server, optionally enabling debugger attachment.
func main() {
	debug := flag.Bool("debug", false, "Enable debugger support")
	flag.Parse()
	if err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/netcheck-de/redshift",
		Debug:   *debug,
	}); err != nil {
		log.Fatal(err)
	}
}
