package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDebugGrantCrossCatalogTransientReadPreservesState(t *testing.T) {
	r := newDebugGrantResource().(*debugGrantResource)
	r.resourceClient = testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, statement string, _ map[string]string) ([]sqlclient.Row, error) {
		if statement == readDebugGrantAuthStatement() {
			return []sqlclient.Row{{"auth_method": "IAM AssumeUser"}}, nil
		}
		if strings.HasPrefix(statement, "SHOW GRANTS") {
			return nil, errors.New("temporary catalog failure")
		}
		// A successful but incomplete listing is not evidence that a cross-catalog database was dropped.
		return nil, nil
	}))
	data := debugGrantModel{Database: types.StringValue("sales@catalog"), Grantee: types.StringValue("IAM:auditor"), GranteeType: types.StringValue("USER")}
	state := testState(t, r, data)
	response := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	require.True(t, response.Diagnostics.HasError())
	assert.False(t, response.State.Raw.IsNull())
	assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "not visible")
}
