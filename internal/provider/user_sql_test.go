package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userWith returns a copy of base after change, for building golden cases from one representative user.
func userWith(base userModel, change func(*userModel)) userModel {
	change(&base)
	return base
}

// userStrings builds a string list attribute.
func userStrings(values ...string) types.List {
	elements := make([]attr.Value, len(values))
	for i, value := range values {
		elements[i] = types.StringValue(value)
	}
	return types.ListValueMust(types.StringType, elements)
}

// userDefaults builds a session_defaults map attribute.
func userDefaults(values map[string]string) types.Map {
	elements := map[string]attr.Value{}
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

// userAllOptions sets every option the user resource renders, with values that need quoting.
func userAllOptions(data *userModel) {
	data.ValidUntil = types.StringValue("2030-06-30T23:59:00+02:00")
	data.ConnectionLimit = types.Int64Value(10)
	data.SessionTimeout = types.Int64Value(300)
	data.SyslogAccess = types.StringValue("UNRESTRICTED")
	data.SearchPath = userStrings("$user", `Odd"Schema`, `it's \path`)
	data.SessionDefaults = userDefaults(map[string]string{"timezone": "Europe/Berlin", "query_group": `it's \x`})
}

// userIdentityProvider makes the user an IdP-linked user with a disabled password and an external ID that needs
// quoting.
func userIdentityProvider(data *userModel) {
	data.PasswordDisabled = types.BoolValue(true)
	data.ExternalID = types.StringValue(`Ext"ID-ABC`)
}

// TestUserSQL pins every user statement, including quoting of identifiers, secrets, and option values.
func TestUserSQL(t *testing.T) {
	quoted := userWith(grafanaUser(), func(data *userModel) { data.Name = types.StringValue(`Odd"User`) })
	create := func(data userModel, secret string) func() ([]string, error) {
		return func() ([]string, error) { return createUserStatements(data, secret) }
	}
	alter := func(prev, plan userModel) func() ([]string, error) {
		return func() ([]string, error) { return alterUserStatements(prev, plan) }
	}
	rotated := func(data *userModel) {
		data.PasswordVersion, data.Password = types.Int64Value(1), types.StringValue(`it's \new`)
	}
	privileged := func(data *userModel) { data.Superuser, data.CreateDB = types.BoolValue(true), types.BoolValue(true) }
	disabled := func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) }
	enabled := func(data *userModel) {
		data.PasswordDisabled, data.Password = types.BoolValue(false), types.StringValue(`it's \back`)
	}
	allOptions := userWith(quoted, userAllOptions)
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "user", []sqlCase{
		{"create", create(grafanaUser(), "InitialPass123")},
		{"create_superuser_createdb", create(userWith(grafanaUser(), privileged), "InitialPass123")},
		{"create_createdb_only", create(userWith(grafanaUser(), func(data *userModel) { data.CreateDB = types.BoolValue(true) }), "InitialPass123")},
		{"create_quoted", create(quoted, `it's \secret`)},
		{"create_password_disabled", create(userWith(grafanaUser(), disabled), "")},
		{"create_all_options", create(allOptions, `it's \secret`)},
		{"create_identity_provider", create(userWith(quoted, userIdentityProvider), "")},
		{"create_unlimited_no_timeout", create(userWith(grafanaUser(), func(data *userModel) {
			data.ConnectionLimit, data.SessionTimeout, data.ValidUntil = types.Int64Value(-1), types.Int64Value(0), types.StringValue("infinity")
		}), "InitialPass123")},
		{"create_invalid_syslog", create(userWith(grafanaUser(), func(data *userModel) { data.SyslogAccess = types.StringValue("OPEN; DROP") }), "InitialPass123")},
		{"create_invalid_valid_until", create(userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringValue("2030-13-01T00:00:00Z") }), "InitialPass123")},
		{"create_fractional_valid_until", create(userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringValue("2030-01-01T00:00:00.5Z") }), "InitialPass123")},
		{"create_invalid_session_parameter", create(userWith(grafanaUser(), func(data *userModel) {
			data.SessionDefaults = userDefaults(map[string]string{"search_path": "public"})
		}), "InitialPass123")},
		{"alter_unchanged", alter(grafanaUser(), grafanaUser())},
		{"alter_unchanged_all_options", alter(allOptions, allOptions)},
		{"alter_rotate_password", alter(quoted, userWith(quoted, rotated))},
		{"alter_rotate_without_password", alter(grafanaUser(), userWith(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_rotate_from_null_version", alter(userWith(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Null() }), userWith(grafanaUser(), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_password_unchanged_version", alter(grafanaUser(), userWith(grafanaUser(), func(data *userModel) { data.Password = types.StringValue("ignored") }))},
		{"alter_grant_capabilities", alter(grafanaUser(), userWith(grafanaUser(), privileged))},
		{"alter_revoke_capabilities", alter(userWith(quoted, privileged), quoted)},
		{"alter_capabilities_null_prior", alter(userWith(grafanaUser(), func(data *userModel) { data.Superuser, data.CreateDB = types.BoolNull(), types.BoolNull() }), userWith(grafanaUser(), privileged))},
		{"alter_all", alter(grafanaUser(), userWith(userWith(grafanaUser(), privileged), rotated))},
		{"alter_disable_password", alter(quoted, userWith(quoted, disabled))},
		{"alter_disable_password_and_revoke_superuser", alter(userWith(grafanaUser(), privileged), userWith(grafanaUser(), disabled))},
		{"alter_disable_password_and_set_external_id", alter(quoted, userWith(quoted, userIdentityProvider))},
		{"alter_enable_password", alter(userWith(quoted, disabled), userWith(quoted, enabled))},
		{"alter_enable_password_and_grant_superuser", alter(userWith(grafanaUser(), disabled), userWith(userWith(grafanaUser(), enabled), privileged))},
		{"alter_enable_password_and_rotate", alter(userWith(grafanaUser(), disabled), userWith(userWith(grafanaUser(), enabled), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_enable_without_password", alter(userWith(grafanaUser(), disabled), userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(false) }))},
		{"alter_rotate_while_disabled", alter(userWith(grafanaUser(), disabled), userWith(userWith(grafanaUser(), disabled), func(data *userModel) { data.PasswordVersion = types.Int64Value(1) }))},
		{"alter_disabled_from_null_prior", alter(userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolNull() }), userWith(grafanaUser(), disabled))},
		{"alter_enabled_from_null_prior", alter(userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolNull() }), grafanaUser())},
		{"alter_disabled_from_null_prior_with_external_id", alter(
			userWith(userWith(grafanaUser(), privileged), func(data *userModel) { data.PasswordDisabled = types.BoolNull() }),
			userWith(grafanaUser(), userIdentityProvider),
		)},
		{"alter_set_all_options", alter(quoted, allOptions)},
		{"alter_clear_all_options", alter(allOptions, userWith(quoted, func(data *userModel) {
			data.ConnectionLimit, data.SessionTimeout, data.SyslogAccess = types.Int64Value(-1), types.Int64Value(0), types.StringValue("RESTRICTED")
		}))},
		{"alter_change_external_id", alter(userWith(quoted, userIdentityProvider), userWith(userWith(quoted, userIdentityProvider), func(data *userModel) {
			data.ExternalID = types.StringValue("other")
		}))},
		{"alter_unmanaged_external_id", alter(userWith(quoted, userIdentityProvider), userWith(userWith(quoted, userIdentityProvider), func(data *userModel) {
			data.ExternalID = types.StringNull()
		}))},
		{"alter_valid_until_equivalent_never", alter(userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringNull() }), grafanaUser())},
		{"alter_fractional_valid_until", alter(grafanaUser(), userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringValue("2030-01-01T00:00:00.5Z") }))},
		{"alter_session_defaults_diff", alter(
			userWith(grafanaUser(), func(data *userModel) {
				data.SessionDefaults = userDefaults(map[string]string{"timezone": "UTC", "statement_timeout": "1000", "datestyle": "ISO, MDY"})
			}),
			userWith(grafanaUser(), func(data *userModel) {
				data.SessionDefaults = userDefaults(map[string]string{"timezone": "Europe/Berlin", "datestyle": "ISO, MDY", "query_group": "etl"})
			}),
		)},
		{"alter_search_path_reorder", alter(
			userWith(grafanaUser(), func(data *userModel) { data.SearchPath = userStrings("public", "analytics") }),
			userWith(grafanaUser(), func(data *userModel) { data.SearchPath = userStrings("analytics", "public") }),
		)},
		{"alter_invalid_session_parameter", alter(grafanaUser(), userWith(grafanaUser(), func(data *userModel) {
			data.SessionDefaults = userDefaults(map[string]string{"Bad Name": "x"})
		}))},
		{"drop", func() string { return dropUserStatement(grafanaUser()) }},
		{"drop_quoted", func() string { return dropUserStatement(quoted) }},
		{"read", func() (string, error) {
			sql, parameters, err := readUserQuery(quoted).Build()
			assert.Equal(t, map[string]string{"name": `Odd"User`}, parameters)
			return sql, err
		}},
		{"read_info", built(readUserInfoQuery(quoted))},
	})
}

// TestUserAlterCoverage keeps an update step for every in-place user attribute; the secret only feeds rotation.
func TestUserAlterCoverage(t *testing.T) {
	assertAlterCoverage(t, newUserResource(), userAlterSteps, "password_wo")
}

// TestUserReadQueryRejectsEmptyName fails before sending a binding the Data API would reject.
func TestUserReadQueryRejectsEmptyName(t *testing.T) {
	_, _, err := readUserQuery(userModel{Name: types.StringValue("")}).Build()
	require.ErrorContains(t, err, ":name is empty")
	_, _, err = readUserInfoQuery(userModel{Name: types.StringValue("")}).Build()
	require.ErrorContains(t, err, ":name is empty")
}

// TestUserAlterBatchOrder checks the ordering Redshift requires: a password is disabled only after superuser is
// revoked, and enabled before superuser is granted; options such as external_id always come last.
func TestUserAlterBatchOrder(t *testing.T) {
	admin := userWith(grafanaUser(), func(data *userModel) { data.Superuser = types.BoolValue(true) })
	disabled := userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) })
	summaries := func(batches []userBatch) []string {
		var names []string
		for _, batch := range batches {
			if len(batch.statements) > 0 {
				names = append(names, batch.summary)
			}
		}
		return names
	}
	batches, err := alterUserBatches(admin, userWith(disabled, userIdentityProvider))
	require.NoError(t, err)
	assert.Equal(t, []string{userUpdateSummary, userPasswordSummary, userUpdateSummary}, summaries(batches))
	enabled := userWith(admin, func(data *userModel) { data.Password = types.StringValue("Secret123") })
	batches, err = alterUserBatches(disabled, enabled)
	require.NoError(t, err)
	assert.Equal(t, []string{userPasswordSummary, userUpdateSummary}, summaries(batches))
}

// TestUserTupleError pins the combinations rejected before SQL runs and the ones deferred or allowed.
func TestUserTupleError(t *testing.T) {
	unknownDisabled := userWith(grafanaUser(), func(data *userModel) {
		data.PasswordDisabled, data.ExternalID = types.BoolUnknown(), types.StringValue("abc")
	})
	for name, test := range map[string]struct {
		data          userModel
		secret, check bool
		message       string
	}{
		"password":             {data: grafanaUser(), secret: true},
		"disabled":             {data: userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) })},
		"disabled with secret": {data: userWith(grafanaUser(), func(data *userModel) { data.PasswordDisabled = types.BoolValue(true) }), secret: true, message: "conflicts"},
		"disabled superuser": {data: userWith(grafanaUser(), func(data *userModel) {
			data.PasswordDisabled, data.Superuser = types.BoolValue(true), types.BoolValue(true)
		}), message: "superuser"},
		"external id":           {data: userWith(grafanaUser(), userIdentityProvider), check: true},
		"external id enabled":   {data: userWith(grafanaUser(), func(data *userModel) { data.ExternalID = types.StringValue("abc") }), check: true, message: "external_id"},
		"external id unchanged": {data: userWith(grafanaUser(), func(data *userModel) { data.ExternalID = types.StringValue("abc") })},
		"external id unknown":   {data: unknownDisabled, check: true},
		"valid until":           {data: userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringValue("tomorrow") }), message: "RFC 3339"},
		"valid until fraction":  {data: userWith(grafanaUser(), func(data *userModel) { data.ValidUntil = types.StringValue("2030-01-01T00:00:00.5Z") }), message: "fractional"},
		"timeout":               {data: userWith(grafanaUser(), func(data *userModel) { data.SessionTimeout = types.Int64Value(59) }), message: "session_timeout"},
		"timeout reset":         {data: userWith(grafanaUser(), func(data *userModel) { data.SessionTimeout = types.Int64Value(0) })},
		"limit":                 {data: userWith(grafanaUser(), func(data *userModel) { data.ConnectionLimit = types.Int64Value(-2) }), message: "connection_limit"},
		"syslog":                {data: userWith(grafanaUser(), func(data *userModel) { data.SyslogAccess = types.StringValue("ALL") }), message: "syslog_access"},
		"session parameter":     {data: userWith(grafanaUser(), func(data *userModel) { data.SessionDefaults = userDefaults(map[string]string{"search_path": "x"}) }), message: "session_defaults"},
	} {
		t.Run(name, func(t *testing.T) {
			err := userTupleError(test.data, test.secret, test.check)
			if test.message == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, test.message)
			}
		})
	}
}
