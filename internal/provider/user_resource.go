package provider

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/mapvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// userResource manages SQL user identity, capabilities, sign-in options, and write-only password rotation.
type userResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// userModel carries Terraform user attributes; write-only passwords are never retained in state.
type userModel struct {
	// ID records the administration binding and user name.
	ID types.String `tfsdk:"id"`
	// Name identifies the database user.
	Name types.String `tfsdk:"name"`
	// Password represents the write-only configuration attribute and stays null in state.
	Password types.String `tfsdk:"password_wo"`
	// PasswordVersion explicitly requests password rotation when incremented.
	PasswordVersion types.Int64 `tfsdk:"password_wo_version"`
	// PasswordDisabled selects PASSWORD DISABLE; it is configuration only, because the catalog does not document it.
	PasswordDisabled types.Bool `tfsdk:"password_disabled"`
	// Superuser controls the SQL CREATEUSER capability.
	Superuser types.Bool `tfsdk:"superuser"`
	// CreateDB controls permission to create databases.
	CreateDB types.Bool `tfsdk:"create_database"`
	// ValidUntil is the password expiration as RFC 3339 or infinity.
	ValidUntil types.String `tfsdk:"valid_until"`
	// ConnectionLimit caps concurrent connections; -1 stands for UNLIMITED.
	ConnectionLimit types.Int64 `tfsdk:"connection_limit"`
	// SessionTimeout is the idle-session timeout in seconds; 0 means none.
	SessionTimeout types.Int64 `tfsdk:"session_timeout"`
	// SyslogAccess is RESTRICTED or UNRESTRICTED.
	SyslogAccess types.String `tfsdk:"syslog_access"`
	// ExternalID links the user to an identity-provider user.
	ExternalID types.String `tfsdk:"external_id"`
	// SearchPath is the stored search_path default in search order.
	SearchPath types.List `tfsdk:"search_path"`
	// SessionDefaults are the other stored session parameter defaults.
	SessionDefaults types.Map `tfsdk:"session_defaults"`
}

var _ = registerResource(newUserResource)

// newUserResource constructs a user lifecycle handler with write-only password support.
func newUserResource() resource.Resource { return &userResource{} }

// Metadata identifies the user resource to Terraform.
func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

// userValidUntilPattern accepts RFC 3339 timestamps without fractional seconds, because the catalog stores the
// expiration as an abstime with whole seconds and a fraction could never be read back. The renderer parses the value
// again, which also rejects impossible dates the pattern lets through.
var userValidUntilPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$`)

// Schema defines user identity, capabilities, sign-in options, stored session defaults, and password rotation.
func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	observed := func(description string) string {
		return description + " When unset, the current catalog value is recorded and left alone. Updated in place."
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a database user. Password input is write-only and rotations require an explicit version change.",
		Attributes: map[string]schema.Attribute{
			"id": idAttribute(),
			"name": schema.StringAttribute{
				Required: true, MarkdownDescription: "Database user name. Changing it replaces the user.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"password_wo": schema.StringAttribute{
				Optional: true, WriteOnly: true, MarkdownDescription: "Write-only password (requires Terraform 1.11 or later); required on creation, on rotation, and when `password_disabled` changes to `false`, and not allowed while `password_disabled` is `true`. Never stored in this resource's plan or state.",
			},
			"password_wo_version": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(0),
				MarkdownDescription: "Password rotation trigger; defaults to `0`. Increment to rotate `password_wo`. Imported users start at version 0 without changing their password. Ignored while `password_disabled` is `true`.",
			},
			"password_disabled": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "`PASSWORD DISABLE`: the user has no password and signs in only with temporary IAM credentials or through a federated identity provider; defaults to `false`. Redshift refuses it for superusers. Changing it to `false` sets the password from `password_wo`. Kept from configuration rather than read back, because Redshift does not document where a disabled password is recorded; import records `false`. Updated in place.",
			},
			"superuser": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "`CREATEUSER` privilege; defaults to `false`. Updated in place.",
			},
			"create_database": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "`CREATEDB` privilege; defaults to `false`. Updated in place.",
			},
			"valid_until": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(userValidUntilInfinity),
				MarkdownDescription: "Password expiration (`VALID UNTIL`) as an RFC 3339 timestamp in whole seconds, such as `2030-01-01T00:00:00Z`, or `infinity` for none; defaults to `infinity`. Only superusers can set an expiration; creating a user without one sends no `VALID UNTIL`. Updated in place.",
				Validators: []validator.String{stringvalidator.Any(
					stringvalidator.OneOf(userValidUntilInfinity),
					stringvalidator.RegexMatches(userValidUntilPattern, "must be an RFC 3339 timestamp in whole seconds"),
				)},
			},
			"connection_limit": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: observed("Maximum concurrent connections (`CONNECTION LIMIT`); `-1` stands for `UNLIMITED`, the Redshift default. Not enforced for superusers."),
				Validators:          []validator.Int64{int64validator.AtLeast(userUnlimitedConnections)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"session_timeout": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: observed(fmt.Sprintf("Idle-session timeout in seconds (`SESSION TIMEOUT`), from %d to %d and applied to new sessions; `0` removes the user's timeout (`RESET SESSION TIMEOUT`) so the warehouse setting applies.", userSessionTimeoutMin, userSessionTimeoutMax)),
				Validators: []validator.Int64{int64validator.Any(
					int64validator.OneOf(0),
					int64validator.Between(userSessionTimeoutMin, userSessionTimeoutMax),
				)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"syslog_access": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: observed("System table visibility (`SYSLOG ACCESS`): `RESTRICTED`, the Redshift default, shows a regular user only their own rows in user-visible system tables and views; `UNRESTRICTED` shows rows of other users too, including the query text of their statements."),
				Validators:          []validator.String{stringvalidator.OneOf(userSyslogNames()...)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"external_id": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Identifier of the user in a native identity provider (`EXTERNALID`); requires `password_disabled = true`. When unset, the current catalog value is recorded and left alone; Redshift has no statement that removes an external ID. Updated in place.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"search_path": schema.ListAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Stored `search_path` default (`ALTER USER ... SET search_path`) as schema names in search order; `$user` stands for the schema named after the session user. It takes effect at the user's next login. Removing it runs `RESET search_path`. Updated in place.",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
					listvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
			"session_defaults": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Other stored session parameter defaults, set with `ALTER USER ... SET parameter TO 'value'` and removed with `RESET parameter`. They take effect at the user's next login. Keys are limited to the documented session parameters `" + strings.Join(userSessionParameterNames(), "`, `") + "`; values are strings as Redshift stores them, such as `Europe/Berlin` or `300000`. Refresh reads the stored defaults from `pg_user.useconfig`, not the values of a running session, and reports these parameters as set outside Terraform too. Updated in place.",
				Validators: []validator.Map{
					mapvalidator.KeysAre(stringvalidator.OneOf(userSessionParameterNames()...)),
					mapvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
				},
			},
		},
	}
}

// userSyslogNames returns the SYSLOG ACCESS levels for the schema validator.
func userSyslogNames() []string {
	names := make([]string, len(userSyslogAccess))
	for i, level := range userSyslogAccess {
		names[i] = string(level)
	}
	return names
}

// userNullModel returns a model for name with typed nulls, which lookups fill from the catalog.
func userNullModel(name types.String) userModel {
	return userModel{
		Name: name, ID: types.StringNull(), Password: types.StringNull(), PasswordVersion: types.Int64Null(),
		PasswordDisabled: types.BoolNull(), Superuser: types.BoolNull(), CreateDB: types.BoolNull(),
		ValidUntil: types.StringNull(), ConnectionLimit: types.Int64Null(), SessionTimeout: types.Int64Null(),
		SyslogAccess: types.StringNull(), ExternalID: types.StringNull(),
		SearchPath: types.ListNull(types.StringType), SessionDefaults: types.MapNull(types.StringType),
	}
}

// userConfigEntries splits the joined useconfig into lower-case parameter names and stored values.
func userConfigEntries(joined string) map[string]string {
	entries := map[string]string{}
	for entry := range strings.SplitSeq(joined, userConfigSeparator) {
		name, value, ok := strings.Cut(entry, "=")
		if ok && name != "" {
			entries[strings.ToLower(name)] = value
		}
	}
	return entries
}

// userParseSearchPath splits a stored search_path into schema names. Redshift stores each configured name as a
// quoted identifier when it needs quoting, so commas inside double quotes do not separate names.
func userParseSearchPath(stored string) []string {
	var schemas []string
	var current strings.Builder
	quoted := false
	flush := func() {
		if text := strings.TrimSpace(current.String()); text != "" {
			schemas = append(schemas, userUnquoteIdentifier(text))
		}
		current.Reset()
	}
	for _, char := range stored {
		switch {
		case char == '"':
			quoted = !quoted
			current.WriteRune(char)
		case char == ',' && !quoted:
			flush()
		default:
			current.WriteRune(char)
		}
	}
	flush()
	return schemas
}

// userUnquoteIdentifier reverses identifier quoting, where a doubled quote stands for one.
func userUnquoteIdentifier(text string) string {
	if len(text) >= 2 && strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) {
		return strings.ReplaceAll(text[1:len(text)-1], `""`, `"`)
	}
	return text
}

// userTimestampLayouts are the renderings of an abstime the transports may return.
var userTimestampLayouts = []string{
	time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07", "2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999",
}

// userParseTimestamp parses a configured or observed expiration; timestamps without an offset are UTC.
func userParseTimestamp(text string) (time.Time, bool) {
	for _, layout := range userTimestampLayouts {
		if instant, err := time.Parse(layout, text); err == nil {
			return instant, true
		}
	}
	return time.Time{}, false
}

// userObservedValidUntil keeps the configured spelling when the catalog holds the same expiration. The catalog shows
// no expiration as null or infinity; both are recorded as infinity, the schema default, so an import or state written
// before the attribute existed plans no change against a configuration that omits it or spells out infinity.
func userObservedValidUntil(current types.String, observed string) types.String {
	configured := knownString(current)
	if observed == "" || strings.EqualFold(observed, userValidUntilInfinity) {
		if strings.EqualFold(configured, userValidUntilInfinity) {
			return current
		}
		return types.StringValue(userValidUntilInfinity)
	}
	instant, ok := userParseTimestamp(observed)
	if !ok {
		return types.StringValue(observed)
	}
	if wanted, ok := userParseTimestamp(configured); ok && wanted.Equal(instant) {
		return current
	}
	return types.StringValue(instant.UTC().Format(time.RFC3339))
}

// userObservedSearchPath returns the stored search_path, or null when none is stored.
func userObservedSearchPath(entries map[string]string) types.List {
	stored, ok := entries[userSearchPathParameter]
	if !ok {
		return types.ListNull(types.StringType)
	}
	schemas := userParseSearchPath(stored)
	values := make([]attr.Value, len(schemas))
	for i, schema := range schemas {
		values[i] = types.StringValue(schema)
	}
	return types.ListValueMust(types.StringType, values)
}

// userObservedSessionDefaults returns the stored allowlisted defaults. A configured empty map stays empty rather
// than null, so it does not plan a change.
func userObservedSessionDefaults(current types.Map, entries map[string]string) types.Map {
	values := map[string]attr.Value{}
	for _, name := range userSessionParameterNames() {
		if value, ok := entries[name]; ok {
			values[name] = types.StringValue(value)
		}
	}
	if len(values) == 0 && (current.IsNull() || current.IsUnknown()) {
		return types.MapNull(types.StringType)
	}
	return types.MapValueMust(types.StringType, values)
}

// userInfo applies the SVV_USER_INFO row. Without a row, which regular users get for other users, the values cannot
// be observed, so known values are kept and unknown ones become null.
func userInfo(data *userModel, rows []sqlclient.Row) error {
	if len(rows) != 1 {
		for _, value := range []*types.Int64{&data.ConnectionLimit, &data.SessionTimeout} {
			if value.IsUnknown() {
				*value = types.Int64Null()
			}
		}
		for _, value := range []*types.String{&data.SyslogAccess, &data.ExternalID} {
			if value.IsUnknown() {
				*value = types.StringNull()
			}
		}
		return nil
	}
	row := rows[0]
	limit := int64(userUnlimitedConnections)
	if text := strings.TrimSpace(row["connection_limit"]); text != "" && !strings.EqualFold(text, "UNLIMITED") {
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("decode user connection limit: %w", err)
		}
		limit = parsed
	}
	timeout := int64(0)
	if text := strings.TrimSpace(row["session_timeout"]); text != "" {
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("decode user session timeout: %w", err)
		}
		timeout = parsed
	}
	data.ConnectionLimit, data.SessionTimeout = types.Int64Value(limit), types.Int64Value(timeout)
	data.SyslogAccess = types.StringValue(strings.ToUpper(strings.TrimSpace(row["syslog_access"])))
	if data.SyslogAccess.ValueString() == "" {
		data.SyslogAccess = types.StringValue("RESTRICTED")
	}
	data.ExternalID = types.StringNull()
	if id := row["external_user_id"]; id != "" {
		data.ExternalID = types.StringValue(id)
	}
	return nil
}

// read refreshes user capabilities and options without reading a password.
func (r *userResource) read(ctx context.Context, data *userModel) (bool, error) {
	if err := r.bound(data.ID, r.database.ValueString()); err != nil {
		return false, err
	}
	rows, err := r.selectRows(ctx, r.database.ValueString(), readUserQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	if len(rows) != 1 {
		return false, fmt.Errorf("user %q is ambiguous in the catalog", data.Name.ValueString())
	}
	superuser, err := strconv.ParseBool(rows[0]["usesuper"])
	if err != nil {
		return false, fmt.Errorf("decode user superuser flag: %w", err)
	}
	createDB, err := strconv.ParseBool(rows[0]["usecreatedb"])
	if err != nil {
		return false, fmt.Errorf("decode user CREATEDB flag: %w", err)
	}
	data.Name = types.StringValue(rows[0]["usename"])
	// The catalog does not show a disabled password, so state from an import or from before the attribute existed
	// records the default; a later password_disabled = true then has a known prior and renders PASSWORD DISABLE.
	if data.PasswordDisabled.IsNull() {
		data.PasswordDisabled = types.BoolValue(false)
	}
	data.Superuser = types.BoolValue(superuser)
	data.CreateDB = types.BoolValue(createDB)
	data.ValidUntil = userObservedValidUntil(data.ValidUntil, rows[0]["valuntil"])
	entries := userConfigEntries(rows[0]["useconfig"])
	data.SearchPath = userObservedSearchPath(entries)
	data.SessionDefaults = userObservedSessionDefaults(data.SessionDefaults, entries)
	info, err := r.selectRows(ctx, r.database.ValueString(), readUserInfoQuery(*data))
	if err != nil {
		return false, err
	}
	return true, userInfo(data, info)
}

// userConverged reports whether the observed user matches every planned value the catalog can show.
func userConverged(expected, observed userModel) bool {
	same := func(want, got attr.Value) bool {
		return want.IsUnknown() || want.Equal(got)
	}
	// A null optional-and-computed value leaves the option unmanaged, so any observed value matches. Values from
	// SVV_USER_INFO stay as planned when the row is not visible, so comparing them is safe either way.
	observedOption := func(want, got attr.Value) bool {
		return want.IsNull() || same(want, got)
	}
	return same(expected.Superuser, observed.Superuser) && same(expected.CreateDB, observed.CreateDB) &&
		same(expected.ValidUntil, observed.ValidUntil) && same(expected.SearchPath, observed.SearchPath) &&
		same(expected.SessionDefaults, observed.SessionDefaults) && observedOption(expected.ConnectionLimit, observed.ConnectionLimit) &&
		observedOption(expected.SessionTimeout, observed.SessionTimeout) && observedOption(expected.SyslogAccess, observed.SyslogAccess) &&
		observedOption(expected.ExternalID, observed.ExternalID)
}

// userSecret reads the write-only password from configuration, because plans never carry it.
func userSecret(ctx context.Context, config tfsdk.Config) (string, diag.Diagnostics) {
	var value types.String
	diagnostics := config.GetAttribute(ctx, path.Root("password_wo"), &value)
	return knownString(value), diagnostics
}

// ValidateConfig rejects option combinations Redshift refuses at plan time; unknown values are checked at apply.
func (r *userResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := userTupleError(data, !data.Password.IsNull(), true); err != nil {
		resp.Diagnostics.AddError("Invalid Redshift user", err.Error())
	}
}

// Create creates the user and verifies its requested capabilities and options.
func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	secret, diagnostics := userSecret(ctx, req.Config)
	if err := userTupleError(data, secret != "", true); err != nil {
		resp.Diagnostics.AddError("Invalid Redshift user", err.Error())
		return
	}
	if !userPasswordDisabled(data) && (diagnostics.HasError() || secret == "") {
		resp.Diagnostics.AddError("Create Redshift user", "password_wo must be set when creating a user unless password_disabled is true")
		return
	}
	statements, err := createUserStatements(data, secret)
	if err == nil {
		err = r.exec(ctx, r.database.ValueString(), statements...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create Redshift user", err.Error())
		return
	}
	data.ID = r.identity(r.database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	expected := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift user", err.Error())
		return
	}
	if !found || !userConverged(expected, data) {
		resp.Diagnostics.AddError("Verify Redshift user", "The user is absent or its privileges or options do not match after creation.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes non-secret user attributes or removes a missing user from state.
func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read Redshift user", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update rotates or re-enables a password, applies the other changed options, and verifies convergence.
func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, previous userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &previous)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.bound(previous.ID, r.database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update Redshift user", err.Error())
		return
	}
	// The plan never carries the write-only secret, so a rotation or re-enable reads it from configuration.
	desired := data
	if userRotates(previous, data) || userEnables(previous, data) {
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("password_wo"), &desired.Password)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	if err := userTupleError(desired, false, !previous.ExternalID.Equal(data.ExternalID)); err != nil {
		resp.Diagnostics.AddError("Invalid Redshift user", err.Error())
		return
	}
	batches, err := alterUserBatches(previous, desired)
	if err != nil {
		summary := userUpdateSummary
		if errors.Is(err, errUserPasswordRequired) || errors.Is(err, errUserPasswordEnable) {
			summary = userPasswordSummary
		}
		resp.Diagnostics.AddError(summary, err.Error()+".")
		return
	}
	for _, batch := range batches {
		if err := r.exec(ctx, r.database.ValueString(), batch.statements...); err != nil {
			resp.Diagnostics.AddError(batch.summary, err.Error())
			return
		}
	}
	expected := data
	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Verify Redshift user", err.Error())
		return
	}
	if !found || !userConverged(expected, data) {
		resp.Diagnostics.AddError("Verify Redshift user", "The user is absent or its privileges or options do not match after updating.")
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete drops the user after checking its binding and verifies removal.
func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, r.database.ValueString(), dropUserStatement(data))
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete Redshift user", "The user remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete Redshift user", err.Error())
	}
}

// ImportState restores user identity without importing its password. The catalog does not show a disabled
// password, so import records the default; configuring password_disabled = true then disables it again, which is
// harmless for a user whose password is already disabled.
func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "name")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("password_wo_version"), int64(0))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("password_disabled"), false)...)
}
