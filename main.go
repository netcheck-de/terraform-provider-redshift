// Package main serves the Redshift SQL Terraform provider.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/netcheck-de/terraform-provider-redshift/internal/provider"
)

// version is supplied by the build linker and defaults to dev for unversioned runs.
var version = "dev"

// serve is replaceable in tests to verify protocol startup without launching a server.
var serve = providerserver.Serve

// fatal is replaceable in tests to verify startup errors without terminating the process.
var fatal = log.Fatal

// main starts the Terraform protocol server, optionally enabling debugger attachment.
func main() {
	debug := flag.Bool("debug", false, "Enable debugger support")
	flag.Parse()
	if err := serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/netcheck-de/redshift",
		Debug:   *debug,
	}); err != nil {
		fatal(err)
	}
}
