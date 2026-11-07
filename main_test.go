package main

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/netcheck-de/terraform-provider-redshift/internal/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProviderProtocolSchema checks that the assembled provider exposes its schemas over protocol 6.
func TestProviderProtocolSchema(t *testing.T) {
	server := providerserver.NewProtocol6WithError(provider.New("test")())
	protocol, err := server()
	require.NoError(t, err)
	response, err := protocol.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	require.NoError(t, err)

	for _, diagnostic := range response.Diagnostics {
		assert.NotEqual(t, tfprotov6.DiagnosticSeverityError, diagnostic.Severity, "%s: %s", diagnostic.Summary, diagnostic.Detail)
	}

	assert.NotNil(t, response.Provider)
	assert.NotEmpty(t, response.ResourceSchemas)
	assert.NotEmpty(t, response.DataSourceSchemas)
}
