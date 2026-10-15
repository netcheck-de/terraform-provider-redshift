package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalSchemaResource manages a Redshift schema mapping to a Glue, Hive, federated, Redshift, or streaming source.
type externalSchemaResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalSchemaModel is the Terraform state for an external schema mapping.
type externalSchemaModel struct {
	// ID records the warehouse/database/schema identity.
	ID types.String `tfsdk:"id"`
	// Database contains the Redshift schema mapping.
	Database types.String `tfsdk:"database"`
	// Name identifies the external SQL schema.
	Name types.String `tfsdk:"name"`
	// SourceType selects the CREATE EXTERNAL SCHEMA ... FROM form; null means DATA_CATALOG.
	SourceType types.String `tfsdk:"source_type"`
	// GlueDatabase selects the AWS Glue Data Catalog database of a DATA_CATALOG schema.
	GlueDatabase types.String `tfsdk:"glue_database"`
	// SourceDatabase selects the Hive, federated, or Redshift database of the other forms.
	SourceDatabase types.String `tfsdk:"source_database"`
	// SourceSchema selects the PostgreSQL or Redshift schema; Redshift defaults it to public.
	SourceSchema types.String `tfsdk:"source_schema"`
	// IAMRoleARN selects the attached role, or comma-separated chain, used to reach the source.
	IAMRoleARN types.String `tfsdk:"iam_role_arn"`
	// Region selects the Glue catalog or stream region; omission uses the warehouse region.
	Region types.String `tfsdk:"region"`
	// URI is the Hive metastore, federated host, or Kafka bootstrap URI.
	URI types.String `tfsdk:"uri"`
	// Port is the Hive metastore or federated database port.
	Port types.Int64 `tfsdk:"port"`
	// SecretARN is the federated credentials secret or the mTLS certificate secret.
	SecretARN types.String `tfsdk:"secret_arn"`
	// Authentication is the streaming authentication mode NONE, IAM, or MTLS, in any case.
	Authentication types.String `tfsdk:"authentication"`
	// AuthenticationARN is the ACM certificate used for mTLS.
	AuthenticationARN types.String `tfsdk:"authentication_arn"`
	// Owner is the SQL user owning the schema; unset configuration keeps the current owner.
	Owner types.String `tfsdk:"owner"`
	// RefreshRevision triggers explicit replacement to rebuild the schema mapping.
	RefreshRevision types.String `tfsdk:"refresh_revision"`
}

var _ = registerResource(newExternalSchemaResource)

// newExternalSchemaResource constructs an external schema handler.
func newExternalSchemaResource() resource.Resource { return &externalSchemaResource{} }

// Metadata identifies the external schema resource to Terraform.
func (r *externalSchemaResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_schema"
}

// externalSchemaInPlace replaces the schema unless ALTER EXTERNAL SCHEMA can change the attribute for the planned
// source type. A source type that cannot be read is treated as replacing so a replacement is never skipped.
func externalSchemaInPlace(attribute string) stringplanmodifier.RequiresReplaceIfFunc {
	return func(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
		var sourceType types.String
		if diagnostics := req.Plan.GetAttribute(ctx, path.Root("source_type"), &sourceType); diagnostics.HasError() || sourceType.IsUnknown() {
			resp.RequiresReplace = true
			return
		}
		source, ok := externalSchemaSources[externalSchemaSourceName(sourceType)]
		removable := attribute == "secret_arn"
		resp.RequiresReplace = !ok || !slices.Contains(source.alterable, attribute) || req.PlanValue.IsNull() && !removable
	}
}

// externalSchemaReplaceUnlessAltered describes externalSchemaInPlace for plans and documentation.
func externalSchemaReplaceUnlessAltered(attribute string, description string) planmodifier.String {
	return stringplanmodifier.RequiresReplaceIf(externalSchemaInPlace(attribute), description, description)
}

// Schema defines the source form, its typed options, owner, and explicit refresh revision.
func (r *externalSchemaResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	computedReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one external schema in a local Redshift database: an AWS Glue Data Catalog, Hive metastore, PostgreSQL or MySQL federated, Redshift cross-database, Kinesis, or MSK source.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local Redshift database owning the external schema. Changing it replaces the external schema."},
			"name":     schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local external schema name. Changing it replaces the external schema."},
			"source_type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(externalSchemaDefaultSource), PlanModifiers: replace,
				Validators:          []validator.String{stringvalidator.OneOf("DATA_CATALOG", "HIVE_METASTORE", "POSTGRES", "MYSQL", "REDSHIFT", "KINESIS", "MSK")},
				MarkdownDescription: "Source form after `FROM`: `DATA_CATALOG` (default; AWS Glue Data Catalog), `HIVE_METASTORE`, `POSTGRES`, `MYSQL`, `REDSHIFT`, `KINESIS`, or `MSK`. It decides which options are required or accepted; see the table on this page. Changing it replaces the external schema.",
			},
			"glue_database": schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "AWS Glue database referenced by a `DATA_CATALOG` schema, which requires it. Changing it replaces the external schema."},
			"source_database": schema.StringAttribute{
				Optional: true, PlanModifiers: replace,
				MarkdownDescription: "External database: the Hive metastore database, the PostgreSQL or MySQL database, or the Redshift database (for example a datashare consumer database). Required by `HIVE_METASTORE`, `POSTGRES`, `MYSQL`, and `REDSHIFT`. Changing it replaces the external schema.",
			},
			"source_schema": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: computedReplace,
				MarkdownDescription: "Schema in the `POSTGRES` or `REDSHIFT` source; Redshift uses `public` when it is omitted. Changing it replaces the external schema.",
			},
			"iam_role_arn": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{externalSchemaReplaceUnlessAltered("iam_role_arn",
					"Updated in place with ALTER EXTERNAL SCHEMA for DATA_CATALOG and MSK schemas; replaces the others.")},
				MarkdownDescription: "IAM role attached to the namespace, or a comma-separated role chain, used to reach the source. Required by every form except `REDSHIFT`, which rejects it, and `MSK`, where it is optional unless `authentication` is `IAM`. Updated in place with `ALTER EXTERNAL SCHEMA ... IAM_ROLE` for `DATA_CATALOG` and `MSK`; changing it replaces other external schemas.",
			},
			"region": schema.StringAttribute{
				// UseStateForUnknown keeps the computed region known during an in-place ALTER, where the framework
				// would otherwise mark it unknown and RequiresReplace would turn the update into a replacement.
				Optional: true, Computed: true, PlanModifiers: computedReplace,
				MarkdownDescription: "AWS Region of the Glue catalog (`DATA_CATALOG`) or stream (`KINESIS`, `MSK`); defaults to the warehouse region and is read from the catalog options. Changing it replaces the external schema.",
			},
			"uri": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{externalSchemaReplaceUnlessAltered("uri",
					"Updated in place with ALTER EXTERNAL SCHEMA for MSK schemas; replaces the others.")},
				MarkdownDescription: "Hive metastore URI or federated database hostname, without a protocol, or the Kafka bootstrap broker URI of an `MSK` schema. Required by `HIVE_METASTORE`, `POSTGRES`, `MYSQL`, and `MSK`. Updated in place with `ALTER EXTERNAL SCHEMA ... URI` for `MSK`; changing it replaces other external schemas.",
			},
			"port": schema.Int64Attribute{
				Optional: true, Computed: true,
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown(), int64planmodifier.RequiresReplace()},
				Validators:          []validator.Int64{int64validator.Between(1, 65535)},
				MarkdownDescription: "Port of a `HIVE_METASTORE` (default 9083), `POSTGRES` (default 5432), or `MYSQL` (default 3306) source. Changing it replaces the external schema.",
			},
			"secret_arn": schema.StringAttribute{
				Optional: true,
				PlanModifiers: []planmodifier.String{externalSchemaReplaceUnlessAltered("secret_arn",
					"Updated in place with ALTER EXTERNAL SCHEMA for MSK schemas; replaces the others.")},
				MarkdownDescription: "AWS Secrets Manager secret ARN: the database credentials that `POSTGRES` and `MYSQL` require, or the mTLS certificate of an `MSK` schema as an alternative to `authentication_arn`. Updated in place for `MSK`; changing it replaces other external schemas.",
			},
			"authentication": schema.StringAttribute{
				Optional: true, Validators: []validator.String{stringvalidator.OneOfCaseInsensitive("NONE", "IAM", "MTLS")},
				MarkdownDescription: "Streaming authentication of an `MSK` schema, which requires it: `NONE`, `IAM`, or `MTLS`, in any case; another case of the same mode is recorded in place without a statement. `MTLS` requires exactly one of `authentication_arn` and `secret_arn`. Updated in place with `ALTER EXTERNAL SCHEMA ... AUTHENTICATION`.",
			},
			"authentication_arn": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "AWS Certificate Manager certificate ARN for `MTLS` authentication of an `MSK` schema. Updated in place together with `authentication`.",
			},
			"owner": schema.StringAttribute{
				Optional: true, Computed: true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				MarkdownDescription: "SQL user owning the external schema, changed after creation and in place with `ALTER SCHEMA ... OWNER TO`. Omit it to keep and report the current owner, which defaults to the creating user. `SVV_EXTERNAL_SCHEMAS` shows a regular user only their own schemas, so managing an external schema owned by another user, including handing it to one, requires a superuser connection.",
			},
			"refresh_revision": schema.StringAttribute{Optional: true, PlanModifiers: replace, MarkdownDescription: "Bump to recreate the external schema after a source catalog change; deletion is restrictive. Changing it replaces the external schema."},
		},
	}
}

// ValidateConfig surfaces options that the source form requires or rejects at plan time; unknown values are
// checked again in Create.
func (r *externalSchemaResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateExternalSchema(data, false); err != nil {
		resp.Diagnostics.AddError("Invalid external schema", err.Error())
	}
}

// externalSchemaKinds maps SVV_EXTERNAL_SCHEMAS.eskind to the source type it was created with.
func externalSchemaKinds() map[string]string {
	kinds := map[string]string{}
	for name, source := range externalSchemaSources {
		for _, kind := range source.kinds {
			kinds[kind] = name
		}
	}
	return kinds
}

// externalSchemaCatalogOptions decodes esoptions with upper-case keys. Values may be JSON strings or numbers; the
// page documents only the IAM_ROLE key, so callers keep their prior value for any key the catalog omits.
func externalSchemaCatalogOptions(text string) (map[string]string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("decode external schema options: %w", err)
	}
	options := map[string]string{}
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			options[strings.ToUpper(key)] = typed
		case float64:
			options[strings.ToUpper(key)] = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			options[strings.ToUpper(key)] = strconv.FormatBool(typed)
		}
	}
	return options, nil
}

// externalSchemaObserved returns the catalog value of an option, or the prior value when the catalog omits it.
// An unknown prior, a computed option left to the server, becomes null.
func externalSchemaObserved(options map[string]string, key string, prior types.String) types.String {
	if value, ok := options[key]; ok && value != "" {
		return types.StringValue(value)
	}
	if prior.IsUnknown() {
		return types.StringNull()
	}
	return prior
}

// read verifies the external schema's form and refreshes its catalog options and owner.
func (r *externalSchemaResource) read(ctx context.Context, data *externalSchemaModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readExternalSchemaQuery(*data))
	if err != nil || len(rows) == 0 {
		return false, err
	}
	row := rows[0]
	if len(rows) == 1 && row["eskind"] == "" {
		return false, fmt.Errorf("schema %q (owner %q) exists, but SVV_EXTERNAL_SCHEMAS does not show it as an external schema to this user: "+
			"it is a local schema, or an external schema owned by another user, whose options only a superuser can read", data.Name.ValueString(), row["owner"])
	}
	kind, supported := externalSchemaKinds()[row["eskind"]]
	if len(rows) != 1 || !supported {
		return false, fmt.Errorf("external schema %q is not a unique schema of a supported source type (eskind %q)", data.Name.ValueString(), row["eskind"])
	}
	if strings.TrimSpace(row["owner"]) == "" {
		return false, fmt.Errorf("external schema %q has no owner in the catalog", data.Name.ValueString())
	}
	options, err := externalSchemaCatalogOptions(row["esoptions"])
	if err != nil {
		return false, err
	}
	if kind == externalSchemaDefaultSource && options["IAM_ROLE"] == "" {
		return false, errors.New("external schema has no IAM_ROLE catalog option")
	}
	observed := *data
	observed.Name = types.StringValue(strings.TrimSpace(row["schemaname"]))
	observed.SourceType = types.StringValue(kind)
	observed.Owner = types.StringValue(strings.TrimSpace(row["owner"]))
	observed.GlueDatabase, observed.SourceDatabase = types.StringNull(), types.StringNull()
	if database := row["databasename"]; database != "" {
		switch {
		case kind == externalSchemaDefaultSource:
			observed.GlueDatabase = types.StringValue(database)
		case slices.Contains(externalSchemaSources[kind].required, "source_database"):
			observed.SourceDatabase = types.StringValue(database)
		}
	}
	observed.IAMRoleARN = externalSchemaObserved(options, "IAM_ROLE", data.IAMRoleARN)
	// An omitted REGION means the warehouse region, which the catalog leaves unrecorded, so an unknown region
	// becomes null while a configured one the catalog does not echo is kept.
	observed.Region = externalSchemaObserved(options, "REGION", data.Region)
	observed.SourceSchema = externalSchemaObserved(options, "SCHEMA", data.SourceSchema)
	observed.URI = externalSchemaObserved(options, "URI", data.URI)
	observed.SecretARN = externalSchemaObserved(options, "SECRET_ARN", data.SecretARN)
	observed.AuthenticationARN = externalSchemaObserved(options, "AUTHENTICATION_ARN", data.AuthenticationARN)
	observed.Authentication = externalSchemaObserved(options, "AUTHENTICATION", data.Authentication)
	if authentication, ok := options["AUTHENTICATION"]; ok && authentication != "" {
		observed.Authentication = keywordValue(data.Authentication, strings.ToUpper(authentication))
	}
	observed.Port = data.Port
	if port, ok := options["PORT"]; ok && port != "" {
		value, err := strconv.ParseInt(port, 10, 64)
		if err != nil {
			return false, fmt.Errorf("external schema %q has an unrecognized PORT %q", data.Name.ValueString(), port)
		}
		observed.Port = types.Int64Value(value)
	} else if observed.Port.IsUnknown() {
		observed.Port = types.Int64Null()
	}
	*data = observed
	return true, nil
}

// externalSchemaConverged reports the first configured attribute the catalog does not hold after Create or Update.
func externalSchemaConverged(expected, observed externalSchemaModel) error {
	names := append([]string{"source_type", "owner"}, externalSchemaOptions...)
	values := func(data externalSchemaModel) map[string]attr.Value {
		values := map[string]attr.Value{"source_type": types.StringValue(externalSchemaSourceName(data.SourceType)), "owner": data.Owner}
		for _, option := range externalSchemaOptions {
			values[option] = externalSchemaOption(data, option)
		}
		return values
	}
	want, got := values(expected), values(observed)
	for _, name := range names {
		if !want[name].IsNull() && !want[name].IsUnknown() && !want[name].Equal(got[name]) {
			return fmt.Errorf("%s is %s in the catalog, not the planned %s", name, got[name], want[name])
		}
	}
	return nil
}

// Create maps the external source into Redshift, assigns the owner, and verifies the catalog options.
func (r *externalSchemaResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := validateExternalSchema(data, true); err != nil {
		resp.Diagnostics.AddError("Invalid external schema", err.Error())
		return
	}
	statement, err := createExternalSchemaStatement(data)
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statement)
	}
	if err != nil {
		resp.Diagnostics.AddError("Create external schema", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	// Computed options left to the server are only known after readback.
	for _, value := range []*types.String{&data.Region, &data.SourceSchema, &data.Owner} {
		if value.IsUnknown() {
			*value = types.StringNull()
		}
	}
	if data.Port.IsUnknown() {
		data.Port = types.Int64Null()
	}
	if data.SourceType.IsNull() {
		data.SourceType = types.StringValue(externalSchemaDefaultSource)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	if !data.Owner.IsNull() {
		if err := r.exec(ctx, data.Database.ValueString(), externalSchemaOwnerStatement(data)); err != nil {
			resp.Diagnostics.AddError("Set external schema owner", err.Error())
			return
		}
	}
	expected := data
	found, err := r.read(ctx, &data)
	if err == nil && !found {
		err = errors.New("the external schema is absent after creation")
	}
	if err == nil {
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
		err = externalSchemaConverged(expected, data)
	}
	if err != nil {
		resp.Diagnostics.AddError("Verify external schema", err.Error())
	}
}

// Read refreshes the external schema or removes a missing schema from state.
func (r *externalSchemaResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read external schema", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// errExternalSchemaVanished reports a schema dropped outside Terraform while an update was planned against it.
var errExternalSchemaVanished = errors.New("the external schema disappeared during the update; refresh the plan")

// Update changes the owner and the options ALTER EXTERNAL SCHEMA supports, starting from the catalog, and
// verifies the result.
func (r *externalSchemaResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data, prior externalSchemaModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	expected := data
	found, err := r.read(ctx, &prior)
	if err == nil && !found {
		err = errExternalSchemaVanished
	}
	var statements []string
	if err == nil {
		statements, err = alterExternalSchemaStatements(prior, data)
	}
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), statements...)
	}
	if err == nil {
		if found, err = r.read(ctx, &data); err == nil && !found {
			err = errExternalSchemaVanished
		}
	}
	if err == nil {
		err = externalSchemaConverged(expected, data)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update external schema", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Delete removes the Redshift schema mapping without deleting the external database.
func (r *externalSchemaResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalSchemaModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropExternalSchemaStatement(data))
		if err == nil {
			found, err = r.read(ctx, &data)
			if err == nil && found {
				resp.Diagnostics.AddError("Delete external schema", "The external schema remains after deletion.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external schema", err.Error())
	}
}

// ImportState restores external schema ownership from its database/name identity.
func (r *externalSchemaResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
