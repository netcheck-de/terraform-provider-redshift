package provider

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var (
	// errUserPasswordRequired rejects a rotation without a secret; the plan never carries write-only values.
	errUserPasswordRequired = errors.New("password_wo is required when password_wo_version changes")
	// errUserPasswordEnable rejects re-enabling password sign-in without a secret, because Redshift enables a
	// password only by setting one.
	errUserPasswordEnable = errors.New("password_wo is required when password_disabled changes to false")
)

// userSearchPathParameter is the session parameter search_path manages; session_defaults may not name it, so
// one parameter never has two owners.
const userSearchPathParameter = "search_path"

// userSessionParameters are the session parameters of the Redshift configuration reference that ALTER USER ... SET
// can store as a user default. Cluster-wide parameters (max_concurrency_scaling_clusters, max_cursor_result_set_size,
// max_failed_login_attempts, use_fips_ssl) and the datashare break-glass switch are left out on purpose.
var userSessionParameters = []sqlclient.Keyword{
	"analyze_threshold_percent",
	"cast_super_null_on_error",
	"datestyle",
	"default_array_search_null_handling",
	"default_geometry_encoding",
	"describe_field_name_in_uppercase",
	"downcase_delimited_identifier",
	"enable_case_sensitive_identifier",
	"enable_case_sensitive_super_attribute",
	"enable_numeric_rounding",
	"enable_result_cache_for_session",
	"enable_spectrum_oid",
	"enable_vacuum_boost",
	"error_on_nondeterministic_update",
	"extra_float_digits",
	"interval_forbid_composite_literals",
	"json_serialization_enable",
	"json_serialization_parse_nested_strings",
	"mv_enable_aqmv_for_session",
	"navigate_super_null_on_error",
	"parse_super_null_on_error",
	"pg_federation_repeatable_read",
	"query_group",
	"spectrum_enable_pseudo_columns",
	"spectrum_query_maxerror",
	"statement_timeout",
	"stored_proc_log_min_messages",
	"timezone",
	"wlm_query_slot_count",
}

// userSessionParameterNames returns the allowlist as strings for schema validators and documentation.
func userSessionParameterNames() []string {
	names := make([]string, len(userSessionParameters))
	for i, name := range userSessionParameters {
		names[i] = string(name)
	}
	return names
}

// userSyslogAccess lists the SYSLOG ACCESS levels.
var userSyslogAccess = []sqlclient.Keyword{"RESTRICTED", "UNRESTRICTED"}

// userConfigSeparator joins useconfig entries in the catalog read. The ASCII record separator cannot occur in a
// configured value, unlike the commas a search_path or datestyle value contains.
const userConfigSeparator = "\x1e"

// userValidUntilInfinity is the VALID UNTIL value for a password that never expires.
const userValidUntilInfinity = "infinity"

// userSessionTimeoutMin and userSessionTimeoutMax bound SESSION TIMEOUT as documented for CREATE and ALTER USER.
const (
	userSessionTimeoutMin = 60
	userSessionTimeoutMax = 1728000
)

// userUnlimitedConnections is the connection_limit value that stands for CONNECTION LIMIT UNLIMITED.
const userUnlimitedConnections = -1

// userAlter starts every ALTER USER statement for the user.
func userAlter(data userModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER USER").Ident(data.Name.ValueString())
}

// userPasswordDisabled reports whether the model asks for PASSWORD DISABLE.
func userPasswordDisabled(data userModel) bool {
	disabled := knownBool(data.PasswordDisabled)
	return disabled != nil && *disabled
}

// userValidUntilLiteral converts the configured expiration into the literal VALID UNTIL parses. Timestamps are sent
// in UTC with an explicit offset, so the result does not depend on the session time zone.
func userValidUntilLiteral(value string) (string, error) {
	if strings.EqualFold(value, userValidUntilInfinity) {
		return userValidUntilInfinity, nil
	}
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", fmt.Errorf("valid_until %q must be an RFC 3339 timestamp or %s", value, userValidUntilInfinity)
	}
	// The catalog keeps whole seconds only, so a fraction would be dropped and never converge.
	if instant.Nanosecond() != 0 {
		return "", fmt.Errorf("valid_until %q must not have fractional seconds, because Redshift stores whole seconds", value)
	}
	return instant.UTC().Format("2006-01-02 15:04:05") + "+00", nil
}

// userConnectionLimit appends CONNECTION LIMIT, where -1 selects UNLIMITED as in the catalog.
func userConnectionLimit(statement sqlclient.Statement, limit int64) sqlclient.Statement {
	if limit == userUnlimitedConnections {
		return statement.Kw("CONNECTION LIMIT UNLIMITED")
	}
	return statement.KwInt("CONNECTION LIMIT", limit)
}

// userSessionParameter maps a session_defaults key to its allowlisted parameter name.
func userSessionParameter(name string) (sqlclient.Keyword, error) {
	parameter, err := sqlclient.OneOf(name, userSessionParameters...)
	if err != nil {
		return "", fmt.Errorf("session_defaults: %w", err)
	}
	return parameter, nil
}

// userTupleError checks the combinations Redshift refuses before any SQL runs. secretSet reports a configured
// password_wo; checkExternalID is false when external_id is not being set, so an observed external ID on an
// otherwise unmanaged user does not fail an unrelated update.
func userTupleError(data userModel, secretSet, checkExternalID bool) error {
	disabled, superuser := userPasswordDisabled(data), knownBool(data.Superuser)
	switch {
	case disabled && secretSet:
		return errors.New("password_wo conflicts with password_disabled = true, because a disabled password cannot be set")
	case disabled && superuser != nil && *superuser:
		return errors.New("password_disabled = true is not allowed for a superuser; Redshift cannot disable a superuser's password")
	case checkExternalID && knownString(data.ExternalID) != "" && !data.PasswordDisabled.IsUnknown() && !disabled:
		return errors.New("external_id requires password_disabled = true")
	}
	if value := knownString(data.ValidUntil); value != "" {
		if _, err := userValidUntilLiteral(value); err != nil {
			return err
		}
	}
	if timeout := knownInt64(data.SessionTimeout); timeout != nil && *timeout != 0 && (*timeout < userSessionTimeoutMin || *timeout > userSessionTimeoutMax) {
		return fmt.Errorf("session_timeout must be 0 or between %d and %d seconds", userSessionTimeoutMin, userSessionTimeoutMax)
	}
	if limit := knownInt64(data.ConnectionLimit); limit != nil && *limit < userUnlimitedConnections {
		return errors.New("connection_limit must be -1 (UNLIMITED) or a nonnegative number")
	}
	if _, err := optOneOf(data.SyslogAccess, userSyslogAccess...); err != nil {
		return fmt.Errorf("syslog_access: %w", err)
	}
	for _, name := range slices.Sorted(maps.Keys(knownMap(data.SessionDefaults))) {
		if _, err := userSessionParameter(name); err != nil {
			return err
		}
	}
	return nil
}

// createUserStatement renders CREATE USER with the configuration secret, which the plan never carries.
// Both capabilities are always spelled out so creation never depends on Redshift defaults; the other options
// appear only when configured, so Redshift's defaults apply to the rest.
func createUserStatement(data userModel, secret string) (string, error) {
	statement := sqlclient.Stmt("CREATE USER").Ident(data.Name.ValueString())
	if userPasswordDisabled(data) {
		statement = statement.Kw("PASSWORD DISABLE")
	} else {
		statement = statement.KwLit("PASSWORD", secret)
	}
	statement = statement.Toggle(data.Superuser.ValueBool(), "CREATEUSER", "NOCREATEUSER").
		Toggle(data.CreateDB.ValueBool(), "CREATEDB", "NOCREATEDB")
	syslog, err := optOneOf(data.SyslogAccess, userSyslogAccess...)
	if err != nil {
		return "", fmt.Errorf("syslog_access: %w", err)
	}
	statement = statement.When(syslog != "", func(s sqlclient.Statement) sqlclient.Statement { return s.Kw("SYSLOG ACCESS", syslog) })
	// No expiration is the Redshift default, and only superusers may use VALID UNTIL, so infinity is left out.
	if value := knownString(data.ValidUntil); value != "" && !strings.EqualFold(value, userValidUntilInfinity) {
		expiration, err := userValidUntilLiteral(value)
		if err != nil {
			return "", err
		}
		statement = statement.KwLit("VALID UNTIL", expiration)
	}
	if limit := knownInt64(data.ConnectionLimit); limit != nil {
		statement = userConnectionLimit(statement, *limit)
	}
	// Zero means no user timeout, which is what omitting the clause gives.
	if timeout := knownInt64(data.SessionTimeout); timeout != nil && *timeout != 0 {
		statement = statement.KwInt("SESSION TIMEOUT", *timeout)
	}
	statement = statement.OptIdent("EXTERNALID", knownString(data.ExternalID))
	return statement.String(), statement.Err()
}

// createUserStatements renders CREATE USER followed by the stored session defaults, which CREATE USER cannot set.
func createUserStatements(data userModel, secret string) ([]string, error) {
	create, err := createUserStatement(data, secret)
	if err != nil {
		return nil, err
	}
	none := data
	none.SearchPath = types.ListNull(types.StringType)
	none.SessionDefaults = types.MapNull(types.StringType)
	defaults, err := userSessionDefaultStatements(none, data)
	if err != nil {
		return nil, err
	}
	return append(append([]string{create}, userSearchPathStatements(none, data)...), defaults...), nil
}

// userListStrings returns the known elements of a list in order, because search_path order is significant.
func userListStrings(value types.List) []string {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	var elements []string
	for _, element := range value.Elements() {
		if text, ok := element.(types.String); ok && !text.IsNull() && !text.IsUnknown() {
			elements = append(elements, text.ValueString())
		}
	}
	return elements
}

// userSearchPathStatements sets the stored search_path default, or resets it when the attribute is removed.
// Each schema is sent as a literal, which Redshift stores as a quoted identifier, so names keep their case.
func userSearchPathStatements(prev, plan userModel) []string {
	if plan.SearchPath.IsUnknown() {
		return nil
	}
	schemas := userListStrings(plan.SearchPath)
	if len(schemas) == 0 {
		if prev.SearchPath.IsNull() || prev.SearchPath.IsUnknown() {
			return nil
		}
		return []string{userAlter(plan).Kw("RESET", userSearchPathParameter).String()}
	}
	items := make([]sqlclient.Statement, len(schemas))
	for i, schema := range schemas {
		items[i] = sqlclient.Lit(schema)
	}
	return []string{userAlter(plan).Kw("SET", userSearchPathParameter, "TO").List(items...).String()}
}

// userSessionDefaultStatements resets the parameters plan drops and sets the ones it adds or changes, one statement
// per parameter in name order.
func userSessionDefaultStatements(prev, plan userModel) ([]string, error) {
	if plan.SessionDefaults.IsUnknown() {
		return nil, nil
	}
	before, after := knownMap(prev.SessionDefaults), knownMap(plan.SessionDefaults)
	names := slices.Sorted(maps.Keys(before))
	for name := range after {
		if _, ok := before[name]; !ok {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	var statements []string
	for _, name := range names {
		value, keep := after[name]
		if old, existed := before[name]; keep && existed && old == value {
			continue
		}
		parameter, err := userSessionParameter(name)
		if err != nil {
			return nil, err
		}
		if !keep {
			statements = append(statements, userAlter(plan).Kw("RESET", parameter).String())
			continue
		}
		statements = append(statements, userAlter(plan).Kw("SET", parameter, "TO").Lit(value).String())
	}
	return statements, nil
}

// userValidUntilValue compares expirations with null and infinity as the same, because both mean no expiration.
func userValidUntilValue(data userModel) attr.Value {
	if data.ValidUntil.IsNull() || strings.EqualFold(data.ValidUntil.ValueString(), userValidUntilInfinity) {
		return types.StringValue(userValidUntilInfinity)
	}
	return data.ValidUntil
}

// userRendered returns the statement text, or nothing when rendering failed; alterUserBatches checks every failure
// first, so a step never drops a statement silently.
func userRendered(statement sqlclient.Statement) []string {
	if statement.Err() != nil {
		return nil
	}
	return []string{statement.String()}
}

// userAlterSteps change one option per statement. Null priors come from import or from state written before the
// attribute existed, which must not assign a password or grant a capability implicitly.
var userAlterSteps = []alterStep[userModel]{
	{
		attribute: "password_disabled",
		value:     func(data userModel) attr.Value { return data.PasswordDisabled },
		// A null prior still renders PASSWORD DISABLE, which needs no secret and is harmless for an already disabled
		// password, but it never re-enables one: that needs a secret, and the password may well be enabled already.
		render: func(prev, plan userModel) []string {
			if userPasswordDisabled(plan) {
				return []string{userAlter(plan).Kw("PASSWORD DISABLE").String()}
			}
			if prev.PasswordDisabled.IsNull() {
				return nil
			}
			return []string{userAlter(plan).KwLit("PASSWORD", plan.Password.ValueString()).String()}
		},
	},
	{
		attribute:     "password_wo_version",
		value:         func(data userModel) attr.Value { return data.PasswordVersion },
		skipNullPrior: true,
		render: func(prev, plan userModel) []string {
			// A disabled password has nothing to rotate, and re-enabling already sets the new secret.
			if userPasswordDisabled(plan) || userEnables(prev, plan) {
				return nil
			}
			return []string{userAlter(plan).KwLit("PASSWORD", plan.Password.ValueString()).String()}
		},
	},
	{
		attribute:     "superuser",
		value:         func(data userModel) attr.Value { return data.Superuser },
		skipNullPrior: true,
		render: func(_, plan userModel) []string {
			return []string{userAlter(plan).Toggle(plan.Superuser.ValueBool(), "CREATEUSER", "NOCREATEUSER").String()}
		},
	},
	{
		attribute:     "create_database",
		value:         func(data userModel) attr.Value { return data.CreateDB },
		skipNullPrior: true,
		render: func(_, plan userModel) []string {
			return []string{userAlter(plan).Toggle(plan.CreateDB.ValueBool(), "CREATEDB", "NOCREATEDB").String()}
		},
	},
	{
		attribute: "syslog_access",
		value:     func(data userModel) attr.Value { return data.SyslogAccess },
		render: func(_, plan userModel) []string {
			level, err := optOneOf(plan.SyslogAccess, userSyslogAccess...)
			if err != nil || level == "" {
				return nil
			}
			return []string{userAlter(plan).Kw("SYSLOG ACCESS", level).String()}
		},
	},
	{
		attribute: "valid_until",
		value:     userValidUntilValue,
		render: func(_, plan userModel) []string {
			value := knownString(plan.ValidUntil)
			if value == "" {
				value = userValidUntilInfinity
			}
			expiration, err := userValidUntilLiteral(value)
			if err != nil {
				return nil
			}
			return []string{userAlter(plan).KwLit("VALID UNTIL", expiration).String()}
		},
	},
	{
		attribute: "connection_limit",
		value:     func(data userModel) attr.Value { return data.ConnectionLimit },
		render: func(_, plan userModel) []string {
			limit := knownInt64(plan.ConnectionLimit)
			if limit == nil {
				return nil
			}
			return userRendered(userConnectionLimit(userAlter(plan), *limit))
		},
	},
	{
		attribute: "session_timeout",
		value:     func(data userModel) attr.Value { return data.SessionTimeout },
		render: func(_, plan userModel) []string {
			timeout := knownInt64(plan.SessionTimeout)
			switch {
			case timeout == nil:
				return nil
			case *timeout == 0:
				return []string{userAlter(plan).Kw("RESET SESSION TIMEOUT").String()}
			}
			return []string{userAlter(plan).KwInt("SESSION TIMEOUT", *timeout).String()}
		},
	},
	{
		attribute: "external_id",
		value:     func(data userModel) attr.Value { return data.ExternalID },
		// Redshift has no statement that removes an external ID, so a cleared value changes nothing.
		render: func(_, plan userModel) []string {
			id := knownString(plan.ExternalID)
			if id == "" {
				return nil
			}
			return []string{userAlter(plan).KwIdent("EXTERNALID", id).String()}
		},
	},
	{
		attribute: "search_path",
		value:     func(data userModel) attr.Value { return data.SearchPath },
		render:    userSearchPathStatements,
	},
	{
		attribute: "session_defaults",
		value:     func(data userModel) attr.Value { return data.SessionDefaults },
		render: func(prev, plan userModel) []string {
			statements, err := userSessionDefaultStatements(prev, plan)
			if err != nil {
				return nil
			}
			return statements
		},
	},
}

// userRotates reports whether plan rotates the password. A null prior version comes from import, which must not
// assign a new password implicitly.
func userRotates(prev, plan userModel) bool {
	before, after := prev.PasswordVersion, plan.PasswordVersion
	return !before.IsNull() && !after.IsUnknown() && !before.Equal(after)
}

// userEnables reports whether plan turns password sign-in back on, which requires setting a password.
func userEnables(prev, plan userModel) bool {
	before := knownBool(prev.PasswordDisabled)
	return before != nil && *before && !userPasswordDisabled(plan) && !plan.PasswordDisabled.IsUnknown()
}

// userDisables reports whether plan turns password sign-in off. A null prior counts as enabled, so the disable still
// runs after a superuser revoke.
func userDisables(prev, plan userModel) bool {
	before := knownBool(prev.PasswordDisabled)
	return (before == nil || !*before) && userPasswordDisabled(plan)
}

// userBatch is a group of ALTER USER statements reported under one diagnostic summary.
type userBatch struct {
	// summary names the step for diagnostics, so a refused rotation reads as such.
	summary string
	// statements run in order.
	statements []string
}

// userPasswordSummary and userUpdateSummary name the password and the other update steps in diagnostics.
const (
	userPasswordSummary = "Rotate Redshift password"
	userUpdateSummary   = "Update Redshift user"
)

// alterUserBatches renders the in-place changes from prev to plan as ordered batches. plan.Password must hold the
// configuration secret, because a rotation or re-enable without one would silently keep the old state.
//
// Redshift cannot disable a superuser's password, and an external ID needs a disabled password, so password changes
// run after the capability toggles when they disable the password and before them otherwise, and the remaining
// options, external_id among them, run last.
func alterUserBatches(prev, plan userModel) ([]userBatch, error) {
	switch {
	case userEnables(prev, plan) && knownString(plan.Password) == "":
		return nil, errUserPasswordEnable
	case userRotates(prev, plan) && !userPasswordDisabled(plan) && !userEnables(prev, plan) && knownString(plan.Password) == "":
		return nil, errUserPasswordRequired
	}
	if _, err := userSessionDefaultStatements(prev, plan); err != nil {
		return nil, err
	}
	if _, err := optOneOf(plan.SyslogAccess, userSyslogAccess...); err != nil {
		return nil, fmt.Errorf("syslog_access: %w", err)
	}
	if value := knownString(plan.ValidUntil); value != "" {
		if _, err := userValidUntilLiteral(value); err != nil {
			return nil, err
		}
	}
	password := userBatch{summary: userPasswordSummary}
	capabilities := userBatch{summary: userUpdateSummary}
	settings := userBatch{summary: userUpdateSummary}
	for _, step := range userAlterSteps {
		statements := alterStatements(prev, plan, []alterStep[userModel]{step})
		switch step.attribute {
		case "password_disabled", "password_wo_version":
			password.statements = append(password.statements, statements...)
		case "superuser", "create_database":
			capabilities.statements = append(capabilities.statements, statements...)
		default:
			settings.statements = append(settings.statements, statements...)
		}
	}
	if userDisables(prev, plan) {
		return []userBatch{capabilities, password, settings}, nil
	}
	return []userBatch{password, capabilities, settings}, nil
}

// alterUserStatements renders every in-place change from prev to plan in execution order.
func alterUserStatements(prev, plan userModel) ([]string, error) {
	batches, err := alterUserBatches(prev, plan)
	var statements []string
	for _, batch := range batches {
		statements = append(statements, batch.statements...)
	}
	return statements, err
}

// dropUserStatement renders DROP USER.
func dropUserStatement(data userModel) string {
	return sqlclient.Stmt("DROP USER").Ident(data.Name.ValueString()).String()
}

// readUserQuery selects the capabilities, expiration, and stored session defaults of one user from pg_user, which
// every user can read and which never exposes the password. useconfig is a text array that neither transport
// decodes, so it is joined into one string.
func readUserQuery(data userModel) sqlclient.Query {
	return sqlclient.Select("usename", "usesuper", "usecreatedb", "valuntil", "array_to_string(useconfig, chr(30)) AS useconfig").From("pg_user").
		Where("usename = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// readUserInfoQuery selects the options only SVV_USER_INFO reports. It is a separate query because joining a
// system view with the leader-node pg_user catalog is not supported, and because regular users see only their
// own row there.
func readUserInfoQuery(data userModel) sqlclient.Query {
	return sqlclient.Select("connection_limit", "syslog_access", "session_timeout", "external_user_id").From("svv_user_info").
		Where("user_name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}
