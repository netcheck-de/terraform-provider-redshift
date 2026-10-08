package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMainEntrypoint checks protocol serving, debug flags, and startup error reporting.
func TestMainEntrypoint(t *testing.T) {
	for _, name := range []string{"serve", "debug", "error"} {
		t.Run(name, func(t *testing.T) {
			oldFlags, oldArgs, oldServe, oldFatal := flag.CommandLine, os.Args, serve, fatal
			t.Cleanup(func() { flag.CommandLine, os.Args, serve, fatal = oldFlags, oldArgs, oldServe, oldFatal })
			flag.CommandLine = flag.NewFlagSet("provider", flag.ContinueOnError)
			os.Args = []string{"terraform-provider-redshift"}
			if name == "debug" {
				os.Args = append(os.Args, "-debug")
			}
			wantError := errors.New("server error")
			served, failed := false, false
			serve = func(ctx context.Context, factory func() provider.Provider, options providerserver.ServeOpts) error {
				served = true
				assert.Equal(t, "registry.terraform.io/netcheck-de/redshift", options.Address)
				assert.Equal(t, name == "debug", options.Debug)
				require.Len(t, factory().Resources(ctx), 19)
				require.Len(t, factory().DataSources(ctx), 19)
				if name == "error" {
					return wantError
				}
				return nil
			}
			fatal = func(values ...any) {
				failed = true
				assert.Equal(t, []any{wantError}, values)
			}
			main()
			assert.True(t, served)
			assert.Equal(t, name == "error", failed)
		})
	}
}
