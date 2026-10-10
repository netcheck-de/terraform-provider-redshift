package main

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The registered type counts are pinned so that adding or dropping a type is a deliberate change that also updates the
// provider index, the README, and the complete example's coverage.
const (
	wantResources   = 36
	wantDataSources = 47
)

// documentedTypes counts the generated documentation pages of one kind, so registering a type without
// regenerating docs fails here instead of shipping an undocumented type.
func documentedTypes(t *testing.T, kind string) int {
	t.Helper()
	pages, err := filepath.Glob(filepath.Join("docs", kind, "*.md"))
	require.NoError(t, err)
	require.NotEmpty(t, pages)
	return len(pages)
}

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
				require.Len(t, factory().Resources(ctx), wantResources)
				require.Len(t, factory().DataSources(ctx), wantDataSources)
				assert.Equal(t, wantResources, documentedTypes(t, "resources"))
				assert.Equal(t, wantDataSources, documentedTypes(t, "data-sources"))
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
