package provider

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// datashareMembershipFixture builds a membership covering every object of one kind in schema serving of datashare
// producer.
func datashareMembershipFixture(kind string) datashareMembershipModel {
	return datashareMembershipModel{Database: types.StringValue("admin"), Datashare: types.StringValue("producer"), Schema: types.StringValue("serving"), Kind: types.StringValue(kind), All: types.BoolValue(true)}
}

var _ = registerReplacementPolicy("redshift_datashare_membership", map[string]replaceRule{"database": replaceAlways, "datashare": replaceAlways, "schema": replaceAlways, "kind": replaceAlways, "all": replaceNever})
var _ = registerLifecycleCase(lifecycleCase{name: "share all tables", new: newDatashareMembershipResource, kind: lifecyclePermission, model: datashareMembershipFixture("TABLES"), setup: func(c *catalog) { c.includeNew = false }, absent: func(c *catalog) { fakeState[*fakeDatashares](c, "datashare").bulkMembers["TABLES"] = nil }, missingRows: func(_ string, sql string) []sqlclient.Row {
	if sql == "SELECT usesuper FROM pg_user WHERE usename = current_user" {
		return []sqlclient.Row{{"usesuper": "true"}}
	}
	return nil
}})

// TestDatashareMembershipFunctionTranscripts pins the SQL of the standard lifecycle for a FUNCTIONS membership.
func TestDatashareMembershipFunctionTranscripts(t *testing.T) {
	fixture := lifecycleCase{name: "share all functions", new: newDatashareMembershipResource, kind: lifecyclePermission, model: datashareMembershipFixture("FUNCTIONS"), setup: func(c *catalog) { c.includeNew = false }, absent: func(c *catalog) { fakeState[*fakeDatashares](c, "datashare").bulkMembers["FUNCTIONS"] = nil }}
	runTranscripts(t, "datashare_membership_functions_lifecycle", newDatashareMembershipResource, standardTranscripts(fixture))
}

// TestDatashareMembershipRepairsCurrentCoverage plans an in-place repair when a new schema object is not shared, and on
// destroy removes every member of its kind while leaving the other kind alone.
func TestDatashareMembershipRepairsCurrentCoverage(t *testing.T) {
	for _, kind := range []string{"TABLES", "FUNCTIONS"} {
		t.Run(kind, func(t *testing.T) {
			c := fullCatalog()
			c.includeNew = false
			f := fakeState[*fakeDatashares](c, "datashare")
			r := &datashareMembershipResource{testResourceClient(c)}
			data := datashareMembershipFixture(kind)
			found, _, _, err := r.read(context.Background(), &data)
			require.NoError(t, err)
			require.True(t, found)
			assert.True(t, data.All.ValueBool())
			otherKind := "FUNCTIONS"
			if kind == "FUNCTIONS" {
				otherKind = "TABLES"
			}
			otherMembers := append([]string(nil), f.bulkMembers[otherKind]...)
			// New objects are not automatically shared. Refresh retains ownership and plans an in-place repair.
			f.bulkCandidates[kind] = append(f.bulkCandidates[kind], "future")
			state := testState(t, r, data)
			refresh := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &refresh)
			require.False(t, refresh.Diagnostics.HasError(), "%v", refresh.Diagnostics)
			require.False(t, refresh.State.Get(context.Background(), &data).HasError())
			assert.False(t, data.All.ValueBool())
			data.All = types.BoolValue(true)
			require.NoError(t, r.apply(context.Background(), &data))
			require.NoError(t, r.verify(context.Background(), &data))
			assert.True(t, data.All.ValueBool())
			// An externally added member belongs to the exclusive owner and is removed on destroy, not adopted
			// into a historical snapshot that would later be mistaken for a different resource's ownership.
			require.False(t, invoke(t, r, "delete", data).HasError())
			assert.Empty(t, f.bulkMembers[kind])
			assert.Equal(t, otherMembers, f.bulkMembers[otherKind], "destroy must preserve the other kind")
		})
	}
}

// TestDatashareMembershipCountsEveryOverload counts one shared function name as incomplete coverage when overloads
// share it, and repairs it on apply.
func TestDatashareMembershipCountsEveryOverload(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	f := fakeState[*fakeDatashares](c, "datashare")
	f.bulkMembers["FUNCTIONS"] = []string{"f_shared"}
	r := &datashareMembershipResource{testResourceClient(c)}
	data := datashareMembershipFixture("FUNCTIONS")
	found, _, count, err := r.read(context.Background(), &data)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 1, count)
	assert.False(t, data.All.ValueBool(), "one opaque name cannot stand for two overload memberships")
	data.All = types.BoolValue(true)
	require.NoError(t, r.apply(context.Background(), &data))
	require.NoError(t, r.verify(context.Background(), &data))
	assert.True(t, data.All.ValueBool())
}

// TestDatashareMembershipRejectsIncludeNewAndAmbiguousNames refuses to mutate a datashare whose schema sets
// include_new, or whose object or schema names contain a dot that makes members ambiguous.
func TestDatashareMembershipRejectsIncludeNewAndAmbiguousNames(t *testing.T) {
	for _, operation := range []string{"create", "update", "delete"} {
		c := fullCatalog()
		c.includeNew = true
		r := &datashareMembershipResource{testResourceClient(c)}
		diagnostics := invoke(t, r, operation, datashareMembershipFixture("TABLES"))
		assert.True(t, diagnostics.HasError(), "%s: %v", operation, diagnostics)
		assert.Empty(t, c.writes)
	}
	c := fullCatalog()
	c.includeNew = false
	fakeState[*fakeDatashares](c, "datashare").bulkCandidates["TABLES"] = []string{"ambiguous.name"}
	r := &datashareMembershipResource{testResourceClient(c)}
	diagnostics := invoke(t, r, "create", datashareMembershipFixture("TABLES"))
	assert.True(t, diagnostics.HasError())
	assert.Empty(t, c.writes)
	invalid := datashareMembershipFixture("TABLES")
	invalid.Schema = types.StringValue("ambiguous.schema")
	var response resource.ValidateConfigResponse
	r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, invalid))}, &response)
	assert.True(t, response.Diagnostics.HasError())
}

// TestDatashareMembershipEmptySchema counts a schema without objects as fully covered.
func TestDatashareMembershipEmptySchema(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	f := fakeState[*fakeDatashares](c, "datashare")
	f.bulkCandidates["TABLES"], f.bulkMembers["TABLES"] = nil, nil
	r := &datashareMembershipResource{testResourceClient(c)}
	data := datashareMembershipFixture("TABLES")
	require.NoError(t, r.verify(context.Background(), &data))
	assert.True(t, data.All.ValueBool())
}

// TestDatashareMembershipPartialKnownValidation reports known invalid schema, kind, and all values at plan time while
// the database is still unknown.
func TestDatashareMembershipPartialKnownValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		change  func(*datashareMembershipModel)
		invalid bool
	}{
		{"unresolved database", func(data *datashareMembershipModel) { data.Database = types.StringUnknown() }, false},
		{"ambiguous known schema", func(data *datashareMembershipModel) { data.Schema = types.StringValue("ambiguous.schema") }, true},
		{"invalid known kind", func(data *datashareMembershipModel) { data.Kind = types.StringValue("PROCEDURES") }, true},
		{"missing acknowledgement", func(data *datashareMembershipModel) { data.All = types.BoolValue(false) }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := newDatashareMembershipResource().(*datashareMembershipResource)
			data := datashareMembershipFixture("TABLES")
			data.Database = types.StringUnknown()
			test.change(&data)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &response)
			assert.Equal(t, test.invalid, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		})
	}
}

// TestDatashareMembershipRestrictedLifecycleRetainsState fails every operation without a superuser, keeping state and
// writing nothing, because a restricted catalog can hide the schema and its members.
func TestDatashareMembershipRestrictedLifecycleRetainsState(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	c.shareSchema = false // An invisible parent must not be mistaken for deletion.
	f := fakeState[*fakeDatashares](c, "datashare")
	f.restricted = true
	f.bulkCandidates["TABLES"] = nil // Nor may an empty visible subset prove complete coverage.
	calls := 0
	r := &datashareMembershipResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		calls++
		assert.Equal(t, "admin", target.Database, "visibility is proved in the provider administration database")
		assert.Equal(t, "SELECT usesuper FROM pg_user WHERE usename = current_user", sql, "restricted lifecycle must not read target catalogs")
		return c.Query(ctx, target, sql, params)
	}))}
	data := datashareMembershipFixture("TABLES")
	data.Database = types.StringValue("producer_database")
	state := testState(t, r, data)
	response := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	require.True(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	assert.Equal(t, state.Raw, response.State.Raw)
	assert.Equal(t, 1, calls)
	for _, operation := range []string{"create", "update", "delete"} {
		diagnostics := invoke(t, r, operation, data)
		assert.True(t, diagnostics.HasError(), "%s: %v", operation, diagnostics)
	}
	assert.Empty(t, c.writes)
}

// TestDatashareMembershipVisibilityProofFailsClosed keeps state when the superuser check returns no identity, an
// unparsable flag, or several rows.
func TestDatashareMembershipVisibilityProofFailsClosed(t *testing.T) {
	for name, rows := range map[string][]sqlclient.Row{
		"missing identity":   nil,
		"invalid boolean":    {{"usesuper": "unknown"}},
		"ambiguous identity": {{"usesuper": "true"}, {"usesuper": "true"}},
	} {
		t.Run(name, func(t *testing.T) {
			r := &datashareMembershipResource{testResourceClient(queryFunc(func(_ context.Context, _ sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
				assert.Equal(t, "SELECT usesuper FROM pg_user WHERE usename = current_user", sql)
				return rows, nil
			}))}
			state := testState(t, r, datashareMembershipFixture("TABLES"))
			response := resource.ReadResponse{State: state}
			r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
			assert.True(t, response.Diagnostics.HasError())
			assert.Equal(t, state.Raw, response.State.Raw)
		})
	}
}

// TestDatashareMembershipMissingProducerAfterVisibilityProof removes the resource once a superuser read shows the
// producer database is gone.
func TestDatashareMembershipMissingProducerAfterVisibilityProof(t *testing.T) {
	r := &datashareMembershipResource{testResourceClient(queryFunc(func(_ context.Context, target sqlclient.Connection, sql string, _ map[string]string) ([]sqlclient.Row, error) {
		assert.Equal(t, "admin", target.Database)
		if sql == "SELECT usesuper FROM pg_user WHERE usename = current_user" {
			return []sqlclient.Row{{"usesuper": "true"}}, nil
		}
		assert.Contains(t, sql, "svv_redshift_databases")
		return nil, nil
	}))}
	data := datashareMembershipFixture("TABLES")
	data.Database = types.StringValue("removed_producer")
	state := testState(t, r, data)
	response := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	assert.True(t, response.State.Raw.IsNull())
}

// TestDatashareMembershipVerificationFailureRemainsRemovable keeps state after ADD succeeded but verification failed,
// so destroy can still remove the members.
func TestDatashareMembershipVerificationFailureRemainsRemovable(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	f := fakeState[*fakeDatashares](c, "datashare")
	f.bulkMembers["TABLES"] = nil
	mutated := false
	r := &datashareMembershipResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, params map[string]string) ([]sqlclient.Row, error) {
		if mutated {
			return nil, errors.New("verification unavailable")
		}
		rows, err := c.Query(ctx, target, sql, params)
		if strings.Contains(sql, " ADD ALL TABLES IN SCHEMA ") {
			mutated = true
		}
		return rows, err
	}))}
	plan := testState(t, r, datashareMembershipFixture("TABLES"))
	response := resource.CreateResponse{State: tfsdk.State{Schema: plan.Schema}}
	r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(plan)}, &response)
	require.True(t, response.Diagnostics.HasError())
	var recorded datashareMembershipModel
	require.False(t, response.State.Get(context.Background(), &recorded).HasError())
	require.NotEmpty(t, recorded.ID.ValueString(), "successful mutation must retain removable ownership")
	r.resourceClient = testResourceClient(c)
	diagnostics := invoke(t, r, "delete", recorded)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, f.bulkMembers["TABLES"])
}

// TestDatashareMembershipPreservesOtherSchema ignores members of other schemas in the same datashare for coverage and
// leaves them in place on destroy.
func TestDatashareMembershipPreservesOtherSchema(t *testing.T) {
	c := fullCatalog()
	c.includeNew = false
	f := fakeState[*fakeDatashares](c, "datashare")
	f.bulkOtherMembers = map[string][]sqlclient.Row{"TABLES": {{"object_name": "other_schema.table"}}}
	r := &datashareMembershipResource{testResourceClient(c)}
	data := datashareMembershipFixture("TABLES")
	require.NoError(t, r.verify(context.Background(), &data), "other schemas must not affect coverage")
	diagnostics := invoke(t, r, "delete", data)
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Empty(t, f.bulkMembers["TABLES"])
	assert.Equal(t, []sqlclient.Row{{"object_name": "other_schema.table"}}, f.bulkOtherMembers["TABLES"])
}

// TestDatashareMembershipValidateConfig rejects empty database, datashare, and schema names.
func TestDatashareMembershipValidateConfig(t *testing.T) {
	r := &datashareMembershipResource{}
	for _, test := range []struct {
		name    string
		change  func(*datashareMembershipModel)
		invalid bool
	}{
		{"valid fully known", func(*datashareMembershipModel) {}, false},
		{"empty database", func(m *datashareMembershipModel) { m.Database = types.StringValue("") }, true},
		{"empty share", func(m *datashareMembershipModel) { m.Datashare = types.StringValue("") }, true},
		{"empty schema", func(m *datashareMembershipModel) { m.Schema = types.StringValue("") }, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := datashareMembershipFixture("TABLES")
			data.ID = types.StringValue("known")
			test.change(&data)
			var response resource.ValidateConfigResponse
			r.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(testState(t, r, data))}, &response)
			assert.Equal(t, test.invalid, response.Diagnostics.HasError(), "%v", response.Diagnostics)
		})
	}
}

// TestDatashareMembershipCoversParallelObjects adds a table created in the schema after the first ADD ALL in another
// round, and still fails, with ownership recorded, when tables keep appearing for every round.
func TestDatashareMembershipCoversParallelObjects(t *testing.T) {
	for _, appearing := range []int{1, snapshotRounds} {
		t.Run(strconv.Itoa(appearing), func(t *testing.T) {
			c := fullCatalog()
			c.includeNew = false
			f := fakeState[*fakeDatashares](c, "datashare")
			f.bulkMembers["TABLES"] = nil
			adds := 0
			r := &datashareMembershipResource{testResourceClient(queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				rows, err := c.Query(ctx, target, sql, parameters)
				if strings.Contains(sql, " ADD ALL TABLES IN SCHEMA ") {
					if adds++; adds <= appearing {
						f.bulkCandidates["TABLES"] = append(f.bulkCandidates["TABLES"], "parallel_"+strconv.Itoa(adds))
					}
				}
				return rows, err
			}))}
			state := testState(t, r, datashareMembershipFixture("TABLES"))
			response := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
			r.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &response)
			var recorded datashareMembershipModel
			require.False(t, response.State.Get(context.Background(), &recorded).HasError())
			assert.True(t, recorded.All.ValueBool())
			assert.False(t, recorded.ID.IsNull(), "ownership is recorded before verification")
			if appearing == snapshotRounds {
				require.True(t, response.Diagnostics.HasError())
				assert.Contains(t, response.Diagnostics.Errors()[0].Detail(), "did not converge")
				assert.Equal(t, snapshotRounds, adds)
				return
			}
			require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
			assert.Equal(t, 2, adds)
			assert.Contains(t, f.bulkMembers["TABLES"], "parallel_1")
		})
	}
}

// TestDatashareMembershipReadWarnsAboutIncludeNew keeps the membership but warns while INCLUDENEW shares future objects implicitly.
func TestDatashareMembershipReadWarnsAboutIncludeNew(t *testing.T) {
	c := fullCatalog()
	c.includeNew = true
	r := &datashareMembershipResource{testResourceClient(c)}
	state := testState(t, r, datashareMembershipFixture("TABLES"))
	response := resource.ReadResponse{State: state}
	r.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	require.False(t, response.Diagnostics.HasError(), "%v", response.Diagnostics)
	require.Len(t, response.Diagnostics.Warnings(), 1)
	assert.Contains(t, response.Diagnostics.Warnings()[0].Detail(), "include_new = false")
	assert.False(t, response.State.Raw.IsNull())
}
