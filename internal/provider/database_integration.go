package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	datasourceschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// databaseIntegrationModel is the zero-ETL integration a destination database receives. SVV_INTEGRATION reports
// the binding, the refresh interval, history mode, and automatic remediation; the other three settings have no
// documented catalog column and keep their configured values.
type databaseIntegrationModel struct {
	// IntegrationID is the zero-ETL integration identifier from SVV_INTEGRATION.
	IntegrationID types.String `tfsdk:"integration_id"`
	// SourceDatabase is the replicated source database, required for PostgreSQL and Oracle sources.
	SourceDatabase types.String `tfsdk:"source_database"`
	// RefreshInterval is the wait in seconds between refresh cycles.
	RefreshInterval types.Int64 `tfsdk:"refresh_interval"`
	// HistoryMode keeps the history of changed rows in new tables.
	HistoryMode types.Bool `tfsdk:"history_mode"`
	// AutoRemediation resynchronizes tables affected by duplicate rows.
	AutoRemediation types.Bool `tfsdk:"auto_remediation"`
	// QueryAllStates allows queries on tables in every replication state; not read back.
	QueryAllStates types.Bool `tfsdk:"query_all_states"`
	// AcceptInvalidChars replaces invalid VARCHAR characters instead of failing ingestion; not read back.
	AcceptInvalidChars types.Bool `tfsdk:"accept_invalid_chars"`
	// TruncateColumns truncates oversized VARCHAR and SUPER values instead of failing ingestion; not read back.
	TruncateColumns types.Bool `tfsdk:"truncate_columns"`
}

// databaseIntegrationAttributeTypes is the object type of the integration block, for the alter step's change check.
var databaseIntegrationAttributeTypes = map[string]attr.Type{
	"integration_id": types.StringType, "source_database": types.StringType, "refresh_interval": types.Int64Type,
	"history_mode": types.BoolType, "auto_remediation": types.BoolType, "query_all_states": types.BoolType,
	"accept_invalid_chars": types.BoolType, "truncate_columns": types.BoolType,
}

// Bounds of REFRESH_INTERVAL. DynamoDB sources refuse values below 900 seconds, which only Redshift can check
// because the source type is not part of the configuration.
const (
	databaseRefreshIntervalMin = 0
	databaseRefreshIntervalMax = 432000
)

// databaseIntegrationObject converts the block for alterStep, which compares attr.Values; a nil block is null.
func databaseIntegrationObject(integration *databaseIntegrationModel) types.Object {
	if integration == nil {
		return types.ObjectNull(databaseIntegrationAttributeTypes)
	}
	return types.ObjectValueMust(databaseIntegrationAttributeTypes, map[string]attr.Value{
		"integration_id": integration.IntegrationID, "source_database": integration.SourceDatabase,
		"refresh_interval": integration.RefreshInterval, "history_mode": integration.HistoryMode,
		"auto_remediation": integration.AutoRemediation, "query_all_states": integration.QueryAllStates,
		"accept_invalid_chars": integration.AcceptInvalidChars, "truncate_columns": integration.TruncateColumns,
	})
}

// databaseIntegrationReplaceNote explains the block's conditional replacement in plans.
const databaseIntegrationReplaceNote = "Adding or removing the block, or changing `integration_id` or `source_database`, replaces the database."

// databaseIntegrationBlock declares the optional zero-ETL integration. Its fields replace only through the block's
// own modifier, which sees the whole value; integration_id is checked by validateDatabase because the framework
// enforces Required inside a single block even when the block is absent.
func databaseIntegrationBlock() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		MarkdownDescription: "Creates the database as the read-only destination of an active zero-ETL integration with " +
			"`CREATE DATABASE ... FROM INTEGRATION`, which needs a superuser or a user with `CREATEDB` and `CREATEUSER`; " +
			"replication runs with that user's permissions. The warehouse must set `enable_case_sensitive_identifier`. " +
			"Conflicts with `datashare_arn` and `collation`. " + databaseIntegrationReplaceNote + " Refresh reads " +
			"`SVV_INTEGRATION`, which only superusers can see. Create and Update wait up to one minute for readable settings to propagate to the catalog, bounded by any shorter operation timeout.",
		PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplaceIf(databaseIntegrationReplacement, databaseIntegrationReplaceNote, databaseIntegrationReplaceNote)},
		Attributes: map[string]schema.Attribute{
			"integration_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Zero-ETL integration identifier, the `integration_id` of `SVV_INTEGRATION`; required in the block. Changing it replaces the database.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"source_database": schema.StringAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Source database to replicate (`DATABASE`), required for Aurora PostgreSQL, RDS for PostgreSQL, and RDS for Oracle sources. Omit it to report the source database `SVV_INTEGRATION` names. Changing it replaces the database.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"refresh_interval": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Approximate seconds between refresh cycles (`REFRESH_INTERVAL`), from 0 to 432000; DynamoDB sources need at least 900. Omit it to keep and report the current interval. Updated in place with `ALTER DATABASE ... INTEGRATION SET`.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:          []validator.Int64{int64validator.Between(databaseRefreshIntervalMin, databaseRefreshIntervalMax)},
			},
			"history_mode": schema.BoolAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Turn history mode on for all tables (`HISTORY_MODE`). Omit it to keep and report the current setting. Updated in place with `ALTER DATABASE ... INTEGRATION SET`, which switches only tables in the `Synced` state.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"auto_remediation": schema.BoolAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Resynchronize tables affected by duplicate rows automatically (`AUTO_REMEDIATION`). Omit it to keep and report the current setting. Updated in place with `ALTER DATABASE ... INTEGRATION SET`.",
				PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"query_all_states": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Allow queries on tables in every replication state, not only `Synced` (`QUERY_ALL_STATES`). Updated in place with `ALTER DATABASE ... INTEGRATION SET`. No supported catalog readback is used, so changes made outside Terraform are not detected; omitted, it is not managed.",
			},
			"accept_invalid_chars": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Replace invalid VARCHAR characters with `?` instead of stopping ingestion (`ACCEPTINVCHARS`). Updated in place with `ALTER DATABASE ... INTEGRATION SET`. No supported catalog readback is used, so changes made outside Terraform are not detected; omitted, it is not managed.",
			},
			"truncate_columns": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "Truncate VARCHAR and SUPER values that exceed the column instead of stopping ingestion (`TRUNCATECOLUMNS`). Updated in place with `ALTER DATABASE ... INTEGRATION SET`. No supported catalog readback is used, so changes made outside Terraform are not detected; omitted, it is not managed.",
			},
		},
	}
}

// databaseIntegrationLookupAttribute observes the integration in the lookup, which never declares blocks.
func databaseIntegrationLookupAttribute() datasourceschema.SingleNestedAttribute {
	unread := "No supported catalog readback is used, so always null in the lookup."
	return datasourceschema.SingleNestedAttribute{
		Computed: true,
		MarkdownDescription: "Zero-ETL integration of a destination database, from `SVV_INTEGRATION`, which only superusers can see; " +
			"null for shared databases, when the view shows no integration for the database, or with a warning when access to the view is denied.",
		Attributes: map[string]datasourceschema.Attribute{
			"integration_id":       datasourceschema.StringAttribute{Computed: true, MarkdownDescription: "Zero-ETL integration identifier."},
			"source_database":      datasourceschema.StringAttribute{Computed: true, MarkdownDescription: "Replicated source database; null when the view reports none."},
			"refresh_interval":     datasourceschema.Int64Attribute{Computed: true, MarkdownDescription: "Approximate seconds between refresh cycles."},
			"history_mode":         datasourceschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether history mode is on."},
			"auto_remediation":     datasourceschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether tables affected by duplicate rows are resynchronized automatically."},
			"query_all_states":     datasourceschema.BoolAttribute{Computed: true, MarkdownDescription: unread},
			"accept_invalid_chars": datasourceschema.BoolAttribute{Computed: true, MarkdownDescription: unread},
			"truncate_columns":     datasourceschema.BoolAttribute{Computed: true, MarkdownDescription: unread},
		},
	}
}

// databaseIntegrationReplacement replaces the database when the block appears or disappears, or when the binding
// changes; the other fields change in place. A value known only at apply, such as the ARN of an integration replaced
// in the same apply, is a change, because the catalog would still report the old binding. Only a source_database the
// configuration omits is unknown for being computed from the state, so it alone is not a change.
func databaseIntegrationReplacement(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
	if req.StateValue.IsNull() != req.PlanValue.IsNull() {
		resp.RequiresReplace = true
		return
	}
	if req.PlanValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsUnknown() {
		resp.RequiresReplace = true
		return
	}
	planned, prior, configured := req.PlanValue.Attributes(), req.StateValue.Attributes(), req.ConfigValue.Attributes()
	for _, name := range []string{"integration_id", "source_database"} {
		value, ok := planned[name]
		if !ok {
			continue
		}
		if value.IsUnknown() {
			if config, ok := configured[name]; ok && config.IsNull() {
				continue
			}
			resp.RequiresReplace = true
			return
		}
		if !value.Equal(prior[name]) {
			resp.RequiresReplace = true
			return
		}
	}
}

// databaseIntegrationRow decodes one SVV_INTEGRATION row. The character columns are CHAR(128), which pads
// with spaces; an empty source database means the source type has none.
func databaseIntegrationRow(name string, row map[string]string) (databaseIntegrationModel, error) {
	integration := databaseIntegrationModel{
		IntegrationID: types.StringValue(strings.TrimSpace(row["integration_id"])), SourceDatabase: discoveryText(strings.TrimSpace(row["source_database"])),
		RefreshInterval: types.Int64Null(), HistoryMode: types.BoolNull(), AutoRemediation: types.BoolNull(),
		QueryAllStates: types.BoolNull(), AcceptInvalidChars: types.BoolNull(), TruncateColumns: types.BoolNull(),
	}
	if integration.IntegrationID.ValueString() == "" {
		return databaseIntegrationModel{}, fmt.Errorf("SVV_INTEGRATION reports no integration_id for database %q", name)
	}
	if text := strings.TrimSpace(row["refresh_interval"]); text != "" {
		interval, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return databaseIntegrationModel{}, fmt.Errorf("database %q has unrecognized refresh_interval %q", name, row["refresh_interval"])
		}
		integration.RefreshInterval = types.Int64Value(interval)
	}
	for _, flag := range []struct {
		column string
		target *types.Bool
	}{{"is_history_mode", &integration.HistoryMode}, {"auto_remediation", &integration.AutoRemediation}} {
		if text := strings.TrimSpace(row[flag.column]); text != "" {
			value, err := strconv.ParseBool(text)
			if err != nil {
				return databaseIntegrationModel{}, fmt.Errorf("database %q has unrecognized %s %q", name, flag.column, row[flag.column])
			}
			*flag.target = types.BoolValue(value)
		}
	}
	return integration, nil
}

// databaseIntegration reads the integration a destination database receives. A missing row and a permission-denied
// error are distinct: resources must not lose their binding just because the provider identity lost catalog access.
func (r *resourceClient) databaseIntegration(ctx context.Context, name string) (databaseIntegrationModel, bool, error) {
	rows, err := r.selectRows(ctx, r.database.ValueString(), readDatabaseIntegrationQuery(name))
	if err != nil || len(rows) == 0 {
		return databaseIntegrationModel{}, false, err
	}
	if len(rows) != 1 {
		return databaseIntegrationModel{}, false, fmt.Errorf("SVV_INTEGRATION reports %d integrations for database %q", len(rows), name)
	}
	integration, err := databaseIntegrationRow(name, rows[0])
	return integration, err == nil, err
}

// databaseReadIntegration refreshes the integration of a destination database in observed. The binding must match
// the identity in state; the settings no view reports keep their state values. A database SVV_INTEGRATION shows no
// row for keeps existing without the integration, because the integration may have been deleted: refresh must not
// fail on it, and a destroy must still drop the database. Losing superuser access instead returns a permission error.
func (r *resourceClient) databaseReadIntegration(ctx context.Context, state *databaseIntegrationModel, observed *databaseModel) error {
	name := observed.Name.ValueString()
	if observed.DatabaseType.ValueString() == databaseTypeShared {
		return fmt.Errorf("database %q is a shared database, not a zero-ETL destination; migrate it explicitly", name)
	}
	integration, found, err := r.databaseIntegration(ctx, name)
	if err != nil {
		return err
	}
	if !found {
		observed.Integration = nil
		return nil
	}
	if configured := knownString(state.IntegrationID); configured != "" {
		if !strings.EqualFold(configured, integration.IntegrationID.ValueString()) {
			return fmt.Errorf("database %q receives integration %q, not %q; migrate it explicitly", name, integration.IntegrationID.ValueString(), configured)
		}
		integration.IntegrationID = state.IntegrationID
	}
	for _, kept := range []struct{ state, observed *types.Bool }{
		{&state.QueryAllStates, &integration.QueryAllStates},
		{&state.AcceptInvalidChars, &integration.AcceptInvalidChars},
		{&state.TruncateColumns, &integration.TruncateColumns},
	} {
		if !kept.state.IsUnknown() {
			*kept.observed = *kept.state
		}
	}
	observed.Integration = &integration
	return nil
}

// databaseIntegrationClearUnknown nulls the settings the plan left to the catalog, so state written before
// verification is fully known.
func databaseIntegrationClearUnknown(integration *databaseIntegrationModel) {
	if integration == nil {
		return
	}
	if integration.SourceDatabase.IsUnknown() {
		integration.SourceDatabase = types.StringNull()
	}
	if integration.RefreshInterval.IsUnknown() {
		integration.RefreshInterval = types.Int64Null()
	}
	for _, value := range []*types.Bool{&integration.HistoryMode, &integration.AutoRemediation, &integration.QueryAllStates, &integration.AcceptInvalidChars, &integration.TruncateColumns} {
		if value.IsUnknown() {
			*value = types.BoolNull()
		}
	}
}

// databaseIntegrationConverged reports the first configured, readable integration setting the catalog does not hold.
func databaseIntegrationConverged(expected, observed *databaseIntegrationModel) error {
	if expected == nil {
		return nil
	}
	if observed == nil {
		return fmt.Errorf("SVV_INTEGRATION shows no zero-ETL integration for the database; only superusers can see the view")
	}
	for _, option := range []struct {
		name               string
		expected, observed attr.Value
	}{
		{"integration.source_database", expected.SourceDatabase, observed.SourceDatabase},
		{"integration.refresh_interval", expected.RefreshInterval, observed.RefreshInterval},
		{"integration.history_mode", expected.HistoryMode, observed.HistoryMode},
		{"integration.auto_remediation", expected.AutoRemediation, observed.AutoRemediation},
	} {
		if !option.expected.IsNull() && !option.expected.IsUnknown() && !option.expected.Equal(option.observed) {
			return fmt.Errorf("%s is %s in the catalog, not the planned %s", option.name, option.observed, option.expected)
		}
	}
	return nil
}

// databaseIntegrationReadbackWait bounds catalog propagation when no shorter operation timeout is configured.
// Redshift can acknowledge INTEGRATION SET roughly 30 seconds before SVV_INTEGRATION reflects the setting.
const databaseIntegrationReadbackWait = time.Minute

func (r *databaseResource) waitDatabaseIntegration(ctx context.Context, expected databaseModel, observed *databaseModel) error {
	if expected.Integration == nil {
		return nil
	}
	// An absent row during apply is not evidence that the just-created/updated binding was removed. Preserve it
	// even when visibility never converges, so a later ordinary refresh can discover the integration again.
	defer func() {
		if observed.Integration == nil {
			binding := *expected.Integration
			databaseIntegrationClearUnknown(&binding)
			observed.Integration = &binding
		}
	}()
	waitCtx, cancel := context.WithTimeout(ctx, databaseIntegrationReadbackWait)
	defer cancel()
	for {
		convergence := databaseIntegrationConverged(expected.Integration, observed.Integration)
		if convergence == nil {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for SVV_INTEGRATION catalog propagation: %w", convergence)
		case <-time.After(time.Second):
		}
		candidate := *observed
		if candidate.Integration == nil {
			binding := *expected.Integration
			candidate.Integration = &binding
		}
		found, err := r.read(waitCtx, &candidate)
		if err != nil {
			return err
		}
		if !found {
			return errDatabaseVanished
		}
		*observed = candidate
	}
}

// validateDatabaseIntegration rejects an integration block without an identifier or combined with a datashare or a
// collation; unknown values are checked again at apply.
func validateDatabaseIntegration(data databaseModel) error {
	if data.Integration == nil {
		return nil
	}
	switch {
	case !data.DatashareARN.IsNull() && !data.DatashareARN.IsUnknown():
		return fmt.Errorf("the integration block conflicts with datashare_arn; remove one of them")
	case !data.Collation.IsNull() && !data.Collation.IsUnknown():
		return fmt.Errorf("the integration block conflicts with collation; remove one of them")
	case data.Integration.IntegrationID.IsNull():
		return fmt.Errorf("the integration block requires integration_id")
	}
	return nil
}

// databaseTypeDescription describes database_type for the resource and the lookup.
const databaseTypeDescription = "`LOCAL` or `SHARED`; for a zero-ETL destination database, the type `SHOW DATABASES` reports, in uppercase."
