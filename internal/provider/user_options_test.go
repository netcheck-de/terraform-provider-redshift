package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userObservedOptions are the optional-and-computed options as a plan has them before the first read.
func userObservedOptions(data *userModel) {
	data.ConnectionLimit, data.SessionTimeout = types.Int64Unknown(), types.Int64Unknown()
	data.SyslogAccess, data.ExternalID = types.StringUnknown(), types.StringUnknown()
}

// userCreated returns a planned user after a create, with its identity and the options the fake reports.
func userCreated(r *userResource, data userModel) userModel {
	data.ID = r.identity("admin", map[string]string{"name": data.Name.ValueString()})
	return data
}

// runUserState runs a lifecycle operation and returns its diagnostics and the resulting state.
func runUserState(t *testing.T, r *userResource, operation string, plan, previous userModel, secret string) (userModel, diag.Diagnostics) {
	t.Helper()
	ctx := context.Background()
	config := plan
	config.Password = types.StringNull()
	if secret != "" {
		config.Password = types.StringValue(secret)
	}
	var state tfsdk.State
	var diagnostics diag.Diagnostics
	switch operation {
	case "create":
		resp := resource.CreateResponse{State: emptyState(t, r)}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan(testState(t, r, plan)), Config: tfsdk.Config(testState(t, r, config))}, &resp)
		state, diagnostics = resp.State, resp.Diagnostics
	case "update":
		prior := testState(t, r, previous)
		resp := resource.UpdateResponse{State: prior}
		r.Update(ctx, resource.UpdateRequest{Plan: tfsdk.Plan(testState(t, r, plan)), State: prior, Config: tfsdk.Config(testState(t, r, config))}, &resp)
		state, diagnostics = resp.State, resp.Diagnostics
	default:
		prior := testState(t, r, previous)
		resp := resource.ReadResponse{State: prior}
		r.Read(ctx, resource.ReadRequest{State: prior}, &resp)
		state, diagnostics = resp.State, resp.Diagnostics
	}
	var data userModel
	if !diagnostics.HasError() && !state.Raw.IsNull() {
		diagnostics.Append(state.Get(ctx, &data)...)
	}
	return data, diagnostics
}

// TestUserOptionsLifecycle creates an IdP-linked user with every option, changes and clears them, re-enables its
// password, and deletes it, checking the fake catalog and the state after each step.
func TestUserOptionsLifecycle(t *testing.T) {
	c := &catalog{}
	r := &userResource{testResourceClient(c)}
	planned := userWith(userWith(grafanaUser(), userAllOptions), userObservedOptions)
	planned = userWith(planned, func(data *userModel) {
		data.PasswordDisabled, data.ExternalID = types.BoolValue(true), types.StringValue(`Ext"ID`)
		data.ConnectionLimit, data.SessionTimeout = types.Int64Value(10), types.Int64Value(300)
	})
	state, diagnostics := runUserState(t, r, "create", planned, planned, "")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	family := fakeState[*userFamily](c, "user")
	assert.True(t, family.disabled)
	assert.Equal(t, `Ext"ID`, family.externalID)
	assert.Contains(t, family.config, `search_path="$user", "Odd""Schema", "it's \path"`)
	assert.Equal(t, "RESTRICTED", state.SyslogAccess.ValueString(), "an unconfigured option records the Redshift default")
	assert.Equal(t, planned.ValidUntil, state.ValidUntil, "an equal expiration keeps its configured spelling")
	assert.Equal(t, planned.SearchPath, state.SearchPath)
	assert.Equal(t, planned.SessionDefaults, state.SessionDefaults)

	changed := userWith(state, func(data *userModel) {
		data.ValidUntil, data.SearchPath = types.StringValue(userValidUntilInfinity), userStrings("analytics")
		data.SessionDefaults = userDefaults(map[string]string{"timezone": "UTC"})
		data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(-1), types.Int64Value(0), types.StringValue("RESTRICTED")
	})
	updated, diagnostics := runUserState(t, r, "update", changed, state, "")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, changed, updated)
	assert.Equal(t, "infinity", family.validUntil)
	assert.ElementsMatch(t, []string{"timezone=UTC", "search_path=analytics"}, family.config)

	// The external ID stays observed rather than configured, so re-enabling the password does not set it again.
	enabled := userWith(updated, func(data *userModel) { data.PasswordDisabled = types.BoolValue(false) })
	_, diagnostics = runUserState(t, r, "update", enabled, updated, "")
	require.True(t, diagnostics.HasError(), "re-enabling needs a secret")
	assert.Equal(t, userPasswordSummary, diagnostics.Errors()[0].Summary())
	_, diagnostics = runUserState(t, r, "update", enabled, updated, "Secret123")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.False(t, family.disabled)
	assert.Contains(t, c.writes, `ALTER USER "grafana" PASSWORD 'Secret123'`)

	require.False(t, runUser(t, r, "delete", enabled, enabled, "").HasError())
	assert.False(t, c.user)
}

// TestUserDisablesPasswordAfterRevokingSuperuser checks the statement order against a fake that, like Redshift,
// refuses to disable a superuser's password and to make a user without a password a superuser.
func TestUserDisablesPasswordAfterRevokingSuperuser(t *testing.T) {
	c := &catalog{user: true, superuser: true}
	r := &userResource{testResourceClient(c)}
	admin := userCreated(r, userWith(grafanaUser(), func(data *userModel) { data.Superuser = types.BoolValue(true) }))
	disabled := userWith(admin, func(data *userModel) {
		data.Superuser, data.PasswordDisabled = types.BoolValue(false), types.BoolValue(true)
	})
	require.False(t, runUser(t, r, "update", disabled, admin, "").HasError())
	assert.Equal(t, []string{`ALTER USER "grafana" NOCREATEUSER`, `ALTER USER "grafana" PASSWORD DISABLE`}, c.writes)
	c.writes = nil
	_, diagnostics := runUserState(t, r, "update", admin, disabled, "Secret123")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{`ALTER USER "grafana" PASSWORD 'Secret123'`, `ALTER USER "grafana" CREATEUSER`}, c.writes)
}

// TestUserCreateValidatesBeforeSQL rejects refused combinations before any statement or state write, and allows a
// disabled password without a secret.
func TestUserCreateValidatesBeforeSQL(t *testing.T) {
	for name, test := range map[string]struct {
		data   userModel
		secret string
	}{
		"disabled with secret": {userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) }), "Secret123"},
		"disabled superuser": {userWith(grafanaUser(), func(data *userModel) {
			data.PasswordDisabled, data.Superuser = types.BoolValue(true), types.BoolValue(true)
		}), ""},
		"external id with password": {userWith(grafanaUser(), func(data *userModel) { data.ExternalID = types.StringValue("abc") }), "Secret123"},
		"missing secret":            {grafanaUser(), ""},
	} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{}
			r := &userResource{testResourceClient(c)}
			state, diagnostics := runUserState(t, r, "create", test.data, test.data, test.secret)
			require.True(t, diagnostics.HasError())
			assert.Empty(t, c.writes)
			assert.True(t, state.Name.IsNull(), "no state is written")
		})
	}
	c := &catalog{}
	r := &userResource{testResourceClient(c)}
	disabled := userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) })
	require.False(t, runUser(t, r, "create", disabled, disabled, "").HasError())
	assert.Equal(t, []string{`CREATE USER "grafana" PASSWORD DISABLE NOCREATEUSER NOCREATEDB`}, c.writes)
}

// TestUserUpdateValidatesBeforeSQL rejects refused combinations and unknown session parameters before any statement.
func TestUserUpdateValidatesBeforeSQL(t *testing.T) {
	for name, change := range map[string]func(*userModel){
		"external id with password": func(data *userModel) { data.ExternalID = types.StringValue("abc") },
		"disabled superuser": func(data *userModel) {
			data.PasswordDisabled, data.Superuser = types.BoolValue(true), types.BoolValue(true)
		},
		"session parameter": func(data *userModel) { data.SessionDefaults = userDefaults(map[string]string{"bogus": "x"}) },
	} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{user: true}
			r := &userResource{testResourceClient(c)}
			prior := userCreated(r, grafanaUser())
			diagnostics := runUser(t, r, "update", userWith(prior, change), prior, "")
			require.True(t, diagnostics.HasError())
			assert.Empty(t, c.writes)
		})
	}
}

// TestUserVerifiesOptionConvergence reports options that the catalog does not show after an acknowledged ALTER.
func TestUserVerifiesOptionConvergence(t *testing.T) {
	for name, change := range map[string]func(*userModel){
		"search path":      func(data *userModel) { data.SearchPath = userStrings("analytics") },
		"session defaults": func(data *userModel) { data.SessionDefaults = userDefaults(map[string]string{"timezone": "UTC"}) },
		"valid until":      func(data *userModel) { data.ValidUntil = types.StringValue("2031-01-01T00:00:00Z") },
		"connection limit": func(data *userModel) { data.ConnectionLimit = types.Int64Value(5) },
		"session timeout":  func(data *userModel) { data.SessionTimeout = types.Int64Value(120) },
		"syslog access":    func(data *userModel) { data.SyslogAccess = types.StringValue("UNRESTRICTED") },
	} {
		t.Run(name, func(t *testing.T) {
			c := &catalog{user: true}
			client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
				if strings.HasPrefix(sql, "ALTER USER") {
					return nil, nil
				}
				return c.Query(ctx, target, sql, parameters)
			})
			r := &userResource{testResourceClient(client)}
			prior := userCreated(r, userWith(grafanaUser(), func(data *userModel) {
				data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(-1), types.Int64Value(0), types.StringValue("RESTRICTED")
			}))
			diagnostics := runUser(t, r, "update", userWith(prior, change), prior, "")
			require.True(t, diagnostics.HasError())
			assert.Equal(t, "Verify Redshift user", diagnostics.Errors()[0].Summary())
		})
	}
}

// TestUserReadReportsDrift records options changed outside Terraform, so the next plan restores them.
func TestUserReadReportsDrift(t *testing.T) {
	c := &catalog{user: true}
	r := &userResource{testResourceClient(c)}
	family := fakeState[*userFamily](c, "user")
	family.validUntil, family.connectionLimit, family.sessionTimeout, family.syslog = "2031-02-03 04:05:06+00", "7", "600", "UNRESTRICTED"
	family.config = []string{"search_path=public, \"Odd\"\"Schema\"", "TimeZone=UTC", "unmanaged_parameter=x"}
	prior := userCreated(r, grafanaUser())
	state, diagnostics := runUserState(t, r, "read", prior, prior, "")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, types.StringValue("2031-02-03T04:05:06Z"), state.ValidUntil)
	assert.Equal(t, types.Int64Value(7), state.ConnectionLimit)
	assert.Equal(t, types.Int64Value(600), state.SessionTimeout)
	assert.Equal(t, types.StringValue("UNRESTRICTED"), state.SyslogAccess)
	assert.Equal(t, userStrings("public", `Odd"Schema`), state.SearchPath)
	assert.Equal(t, userDefaults(map[string]string{"timezone": "UTC"}), state.SessionDefaults, "names are case-insensitive and unmanaged parameters are ignored")
	assert.True(t, state.ExternalID.IsNull())
}

// TestUserReadWithoutUserInfo keeps planned options when SVV_USER_INFO hides the user, as it does for regular users.
func TestUserReadWithoutUserInfo(t *testing.T) {
	c := &catalog{}
	fakeState[*userFamily](c, "user").infoHidden = true
	r := &userResource{testResourceClient(c)}
	planned := userWith(grafanaUser(), userObservedOptions)
	planned.SessionTimeout = types.Int64Value(300)
	state, diagnostics := runUserState(t, r, "create", planned, planned, "Secret123")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, types.Int64Value(300), state.SessionTimeout)
	assert.True(t, state.ConnectionLimit.IsNull())
	assert.True(t, state.SyslogAccess.IsNull())
	assert.True(t, state.ExternalID.IsNull())
}

// TestUserCatalogDecoding pins the decoding of useconfig, search_path, expirations, and SVV_USER_INFO values.
func TestUserCatalogDecoding(t *testing.T) {
	assert.Equal(t, map[string]string{"search_path": `"$user", public`, "datestyle": "ISO, MDY"},
		userConfigEntries(`search_path="$user", public`+userConfigSeparator+"DateStyle=ISO, MDY"+userConfigSeparator+"malformed"))
	assert.Empty(t, userConfigEntries(""))
	assert.Equal(t, []string{"$user", "public", `a,"b`, "Upper"}, userParseSearchPath(`"$user", public, "a,""b", "Upper"`))
	assert.Nil(t, userParseSearchPath(""))

	configured := types.StringValue("2030-01-01T01:00:00+01:00")
	for name, test := range map[string]struct {
		current  types.String
		observed string
		expected types.String
	}{
		"same instant":       {configured, "2030-01-01 00:00:00+00", configured},
		"other instant":      {configured, "2030-01-02 00:00:00+00", types.StringValue("2030-01-02T00:00:00Z")},
		"never from null":    {types.StringNull(), "", types.StringValue("infinity")},
		"never as infinity":  {types.StringValue("infinity"), "", types.StringValue("infinity")},
		"infinity from null": {types.StringNull(), "infinity", types.StringValue("infinity")},
		"expired removed":    {configured, "", types.StringValue("infinity")},
		"undecodable":        {types.StringNull(), "someday", types.StringValue("someday")},
		"without offset":     {types.StringNull(), "2030-01-01 00:00:00", types.StringValue("2030-01-01T00:00:00Z")},
		"direct transport":   {configured, "2030-01-01T00:00:00Z", configured},
	} {
		assert.Equal(t, test.expected, userObservedValidUntil(test.current, test.observed), name)
	}

	for name, row := range map[string]sqlclient.Row{
		"limit":   {"connection_limit": "many", "session_timeout": "0"},
		"timeout": {"connection_limit": "UNLIMITED", "session_timeout": "soon"},
	} {
		data := grafanaUser()
		require.Error(t, userInfo(&data, []sqlclient.Row{row}), name)
	}
	data := grafanaUser()
	require.NoError(t, userInfo(&data, []sqlclient.Row{{"connection_limit": "", "session_timeout": "", "syslog_access": "", "external_user_id": "abc"}}))
	assert.Equal(t, types.Int64Value(-1), data.ConnectionLimit)
	assert.Equal(t, types.Int64Value(0), data.SessionTimeout)
	assert.Equal(t, types.StringValue("RESTRICTED"), data.SyslogAccess)
	assert.Equal(t, types.StringValue("abc"), data.ExternalID)
	assert.Equal(t, types.MapValueMust(types.StringType, map[string]attr.Value{}), userObservedSessionDefaults(types.MapValueMust(types.StringType, map[string]attr.Value{}), nil),
		"a configured empty map stays empty")
}

// TestUserOptionFailures injects a failure at every SQL call of each operation on a user with options, so every
// statement and catalog read reports its error.
func TestUserOptionFailures(t *testing.T) {
	base := userWith(userWith(grafanaUser(), userAllOptions), func(data *userModel) {
		data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(10), types.Int64Value(300), types.StringValue("UNRESTRICTED")
	})
	changed := userWith(base, func(data *userModel) {
		data.SearchPath, data.SessionDefaults = userStrings("analytics"), types.MapNull(types.StringType)
		data.ConnectionLimit, data.PasswordVersion, data.Password = types.Int64Value(3), types.Int64Value(1), types.StringNull()
	})
	for _, operation := range []string{"create", "read", "update", "delete"} {
		t.Run(operation, func(t *testing.T) {
			queries := 0
			for failAt := 0; failAt <= queries; failAt++ {
				c := &catalog{}
				setup := &userResource{testResourceClient(c)}
				prior := userCreated(setup, base)
				if operation != "create" {
					require.False(t, runUser(t, setup, "create", base, base, "Secret123").HasError())
				}
				calls := 0
				client := queryFunc(func(ctx context.Context, target sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
					calls++
					if calls == failAt {
						return nil, errors.New("injected API failure")
					}
					return c.Query(ctx, target, sql, parameters)
				})
				r := &userResource{testResourceClient(client)}
				plan := base
				if operation == "update" {
					plan = userCreated(r, changed)
				}
				diagnostics := runUser(t, r, operation, plan, prior, "Secret456")
				if failAt == 0 {
					require.False(t, diagnostics.HasError(), "%v", diagnostics)
					queries = calls
				} else {
					require.True(t, diagnostics.HasError(), "query %d failure must be reported", failAt)
				}
			}
			assert.GreaterOrEqual(t, queries, 2, "the read alone queries pg_user and svv_user_info")
		})
	}
}

// TestUserOptionTranscripts records the SQL conversation of users with options, the IdP-linked form, and
// password re-enabling.
func TestUserOptionTranscripts(t *testing.T) {
	options := userWith(userWith(grafanaUser(), userAllOptions), func(data *userModel) {
		data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(10), types.Int64Value(300), types.StringValue("UNRESTRICTED")
	})
	federated := userWith(userWith(grafanaUser(), userIdentityProvider), userObservedOptions)
	federated.ExternalID = types.StringValue(`Ext"ID-ABC`)
	disabled := userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) })
	enabled := userWith(grafanaUser(), func(data *userModel) { data.Password = types.StringValue(`Back'Pass\1`) })
	cleared := userWith(grafanaUser(), func(data *userModel) {
		data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(-1), types.Int64Value(0), types.StringValue("RESTRICTED")
	})
	withOptions := func() sqlclient.Client {
		c := &catalog{}
		r := &userResource{testResourceClient(c)}
		require.False(t, runUser(t, r, "create", options, options, "Secret123").HasError())
		return c
	}
	existing := func(c *catalog) func() sqlclient.Client { return func() sqlclient.Client { return c } }
	disabledCatalog := func() sqlclient.Client {
		c := &catalog{user: true}
		fakeState[*userFamily](c, "user").disabled = true
		return c
	}
	secret := func(model userModel, password string) userModel {
		model.Password = types.StringValue(password)
		return model
	}
	runTranscripts(t, "lifecycle/user_options", newUserResource, []transcriptCase{
		{name: "create_options", operation: "create", catalog: existing(&catalog{}), planned: options, config: secret(options, "InitialPass123")},
		{name: "create_identity_provider", operation: "create", catalog: existing(&catalog{}), planned: federated},
		{name: "read_options", operation: "read", catalog: withOptions, prior: options},
		{name: "clear_options", operation: "update", catalog: withOptions, prior: options, planned: cleared},
		{name: "enable_password", operation: "update", catalog: disabledCatalog, prior: disabled, planned: grafanaUser(), config: enabled},
		{name: "import_options", operation: "import", catalog: existing(&catalog{}), planned: options, config: secret(options, "InitialPass123")},
	})
}

// TestUserImportMatchesDefaults imports a user without an expiration and checks that the state equals the plan of a
// configuration that omits valid_until or spells out infinity, so the first plan after import is empty.
func TestUserImportMatchesDefaults(t *testing.T) {
	ctx := context.Background()
	c := &catalog{}
	r := &userResource{testResourceClient(c)}
	created, diagnostics := runUserState(t, r, "create", grafanaUser(), grafanaUser(), "Secret123")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	imported := resource.ImportStateResponse{State: emptyState(t, r)}
	r.ImportState(ctx, resource.ImportStateRequest{ID: created.ID.ValueString()}, &imported)
	require.False(t, imported.Diagnostics.HasError(), "%v", imported.Diagnostics)
	read := resource.ReadResponse{State: imported.State}
	r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	require.False(t, read.Diagnostics.HasError(), "%v", read.Diagnostics)
	var state userModel
	require.False(t, read.State.Get(ctx, &state).HasError())
	planned := userWith(grafanaUser(), func(data *userModel) {
		data.ID, data.ConnectionLimit, data.SessionTimeout = created.ID, types.Int64Value(-1), types.Int64Value(0)
		data.SyslogAccess = types.StringValue("RESTRICTED")
	})
	assert.Equal(t, planned, state)
	assert.Equal(t, types.StringValue(userValidUntilInfinity), state.ValidUntil, "the schema default and the configured spelling")
}

// TestUserReadUpgradesNullPasswordDisabled refreshes state written before password_disabled and valid_until existed
// into their defaults, and checks that password_disabled = true disables the password before setting an external ID
// even when the prior is still null.
func TestUserReadUpgradesNullPasswordDisabled(t *testing.T) {
	c := &catalog{user: true}
	r := &userResource{testResourceClient(c)}
	legacy := userCreated(r, userWith(grafanaUser(), func(data *userModel) {
		data.PasswordDisabled, data.ValidUntil = types.BoolNull(), types.StringNull()
	}))
	state, diagnostics := runUserState(t, r, "read", legacy, legacy, "")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, types.BoolValue(false), state.PasswordDisabled)
	assert.Equal(t, types.StringValue(userValidUntilInfinity), state.ValidUntil)

	federated := userWith(state, func(data *userModel) {
		data.PasswordDisabled, data.ExternalID = types.BoolValue(true), types.StringValue("ext")
	})
	// A plan made without refreshing still carries the null prior, which must disable the password all the same.
	unrefreshed := userWith(state, func(data *userModel) { data.PasswordDisabled = types.BoolNull() })
	_, diagnostics = runUserState(t, r, "update", federated, unrefreshed, "")
	require.False(t, diagnostics.HasError(), "%v", diagnostics)
	assert.Equal(t, []string{`ALTER USER "grafana" PASSWORD DISABLE`, `ALTER USER "grafana" EXTERNALID "ext"`}, c.writes)
	assert.True(t, fakeState[*userFamily](c, "user").disabled)
}
