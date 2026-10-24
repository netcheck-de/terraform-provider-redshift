package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/objectvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// copyJobResource manages a COPY JOB that loads new files from an S3 prefix into a table.
type copyJobResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// copyJobModel is the Terraform state of one copy job.
type copyJobModel struct {
	// ID records the warehouse/database/name identity.
	ID types.String `tfsdk:"id"`
	// Database is the database whose table the job loads.
	Database types.String `tfsdk:"database"`
	// Name identifies the job.
	Name types.String `tfsdk:"name"`
	// Table is the table block: the target schema, table, and optional column list.
	Table types.Object `tfsdk:"table"`
	// Source is the monitored S3 prefix.
	Source types.String `tfsdk:"source"`
	// IAMRole is the role ARN COPY uses to read the source.
	IAMRole types.String `tfsdk:"iam_role"`
	// Format is the COPY data format keyword, or null for pipe-delimited text.
	Format types.String `tfsdk:"format"`
	// FormatArgument is the format's string argument, such as auto for JSON.
	FormatArgument types.String `tfsdk:"format_argument"`
	// Parameters maps UPPERCASE COPY parameter keywords to their arguments.
	Parameters types.Map `tfsdk:"parameters"`
	// Auto selects whether Redshift loads new files automatically.
	Auto types.Bool `tfsdk:"auto"`
}

var _ = registerResource(newCopyJobResource)

// newCopyJobResource constructs a copy job lifecycle handler.
func newCopyJobResource() resource.Resource { return &copyJobResource{} }

// Metadata identifies the copy job resource to Terraform.
func (r *copyJobResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_copy_job"
}

// copyJobDefinitionNote describes the immutable job definition.
const copyJobDefinitionNote = "Changing the COPY definition replaces the job."

// copyJobDefinitionFormat compares the immutable format keyword without case.
func copyJobDefinitionFormat(ctx context.Context, req planmodifier.StringRequest, resp *stringplanmodifier.RequiresReplaceIfFuncResponse) {
	keywordChanged(ctx, req, resp)
}

// Schema defines the job's COPY command, which only replacement changes, and its in-place AUTO setting.
func (r *copyJobResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	definition := replace
	nonEmpty := []validator.String{stringvalidator.LengthAtLeast(1)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a `COPY JOB` that loads files newly created under an Amazon S3 prefix into a table.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": schema.StringAttribute{Required: true, Validators: nonEmpty, PlanModifiers: replace, MarkdownDescription: "Local database containing the target table; the job's statements run there."},
			"name": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				MarkdownDescription: "Job name: lowercase letters, digits, and underscores, without hyphens.",
				Validators:          []validator.String{stringvalidator.RegexMatches(copyJobName, "must be lowercase letters, digits, or underscores, starting with a letter or underscore, at most 127 characters")},
			},
			"source": schema.StringAttribute{
				Required: true, PlanModifiers: definition, Validators: []validator.String{stringvalidator.RegexMatches(copyJobSourcePattern, "must start with s3://")},
				MarkdownDescription: "Amazon S3 prefix the job monitors, such as `s3://bucket/staging/`. Its bucket needs an S3 event integration with the warehouse, also with `auto = false`.",
			},
			"iam_role": schema.StringAttribute{
				Required: true, PlanModifiers: replace, Validators: []validator.String{stringvalidator.RegexMatches(copyJobRolePattern, "must be an IAM role ARN")},
				MarkdownDescription: "ARN of an IAM role associated with the warehouse that can read `source`. COPY JOB does not support the default role.",
			},
			"format": schema.StringAttribute{
				Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIf(copyJobDefinitionFormat, copyJobDefinitionNote, copyJobDefinitionNote)},
				MarkdownDescription: "Data format of `FORMAT AS`: `" + strings.Join(copyFormatNames(), "`, `") + "`. Omit it for pipe-delimited text.",
				Validators:          []validator.String{stringvalidator.OneOfCaseInsensitive(copyFormatNames()...)},
			},
			"format_argument": schema.StringAttribute{
				Optional: true, PlanModifiers: definition,
				MarkdownDescription: "Argument of the format, required for `JSON` and `AVRO` (`auto`, `auto ignorecase`, `noshred`, or an `s3://` JSONPaths file) and `FIXEDWIDTH`. For `SHAPEFILE`, optionally use `SIMPLIFY [AUTO] [tolerance]`, with a finite non-negative numeric tolerance. Rejected for other formats.",
			},
			"parameters": schema.MapAttribute{
				Optional: true, ElementType: types.StringType, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
				MarkdownDescription: "COPY parameters keyed by their UPPERCASE keyword, with `\"\"` for keywords without an argument, such as `{ IGNOREHEADER = \"1\", GZIP = \"\" }`. " +
					"Accepted: `" + strings.Join(copyParameterNames(true), "`, `") + "`. COPY JOB rejects `MAXERROR`, `IGNOREALLERRORS`, `MANIFEST`, and key-based credentials.",
			},
			"auto": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Whether Redshift loads new files automatically (`AUTO ON`); updated in place with `COPY JOB ALTER`. Defaults to `true`, the Redshift default.",
			},
		},
		Blocks: map[string]schema.Block{
			"table": schema.SingleNestedBlock{
				MarkdownDescription: "At least one `table` block is required. Target table of the COPY command.",
				Validators:          []validator.Object{objectvalidator.IsRequired(), objectvalidator.AlsoRequires(path.MatchRelative().AtName("schema"), path.MatchRelative().AtName("name"))},
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.RequiresReplace()},
				Attributes: map[string]schema.Attribute{
					"schema": schema.StringAttribute{Optional: true, Validators: nonEmpty, MarkdownDescription: "Schema of the target table; required in the block."},
					"name":   schema.StringAttribute{Optional: true, Validators: nonEmpty, MarkdownDescription: "Target table; required in the block."},
					"columns": schema.ListAttribute{
						Optional: true, ElementType: types.StringType, Validators: []validator.List{listvalidator.SizeAtLeast(1), listvalidator.NoNullValues()},
						MarkdownDescription: "Columns to load, in the order of the fields in the files; all columns when omitted.",
					},
				},
			},
		},
	}
	foldNames(&resp.Schema, "database", "name")
}

// read checks the binding; a missing database or job is not found.
func (r *copyJobResource) read(ctx context.Context, data *copyJobModel) (bool, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}

	return readCopyJob(ctx, &r.resourceClient, data)
}

// readCopyJob reads one job from SYS_COPY_JOB in its database and stores the readable values in data.
func readCopyJob(ctx context.Context, client *resourceClient, data *copyJobModel) (bool, error) {
	if exists, err := client.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return false, err
	}

	rows, err := client.selectRows(ctx, data.Database.ValueString(), readCopyJobQuery(data.Name.ValueString()))
	if err != nil || len(rows) == 0 {
		return false, err
	}

	if len(rows) != 1 {
		return false, fmt.Errorf("copy job %q has ambiguous catalog metadata", data.Name.ValueString())
	}

	auto, err := copyJobAutoValue(rows[0]["is_auto"])
	if err != nil {
		return false, err
	}

	data.Auto = types.BoolValue(auto)

	data.IAMRole = copyJobRoleValue(data.IAMRole, rows[0]["iam_role"])
	if err := readCopyJobDefinition(data, rows[0]["copy_query"]); err != nil {
		return false, err
	}

	return true, nil
}

// verify checks that the complete planned definition, AUTO setting, and role took effect.
func (r *copyJobResource) verify(ctx context.Context, data *copyJobModel) error {
	planned := *data

	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		return err
	case !found:
		return errors.New("the copy job is absent after apply")
	case !planned.Auto.IsUnknown() && !data.Auto.Equal(planned.Auto):
		return fmt.Errorf("the copy job's auto is %t after apply, expected %t", data.Auto.ValueBool(), planned.Auto.ValueBool())
	case !data.IAMRole.Equal(planned.IAMRole):
		return fmt.Errorf("the copy job uses IAM role %q after apply, expected %q", data.IAMRole.ValueString(), planned.IAMRole.ValueString())
	case !data.Table.Equal(planned.Table) || !data.Source.Equal(planned.Source) || !data.Format.Equal(planned.Format) || !data.FormatArgument.Equal(planned.FormatArgument) || !data.Parameters.Equal(planned.Parameters):
		return errors.New("the copy job definition differs from the configuration after apply")
	}

	return nil
}

// ValidateConfig reports a job Create would reject, during planning once the configuration is known.
func (r *copyJobResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data copyJobModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	// The schema's IsRequired validator already reports an absent table block; repeating it doubles the error.
	if resp.Diagnostics.HasError() || data.Table.IsNull() {
		return
	}

	if err := copyJobValidate(data); err != nil {
		resp.Diagnostics.AddError("Invalid copy job", err.Error())
	}
}

// Create creates the job and verifies it in the catalog.
func (r *copyJobResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data copyJobModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	statement, err := createCopyJobStatement(data)
	if err != nil {
		resp.Diagnostics.AddError("Invalid copy job", err.Error())
		return
	}

	if err := r.exec(ctx, data.Database.ValueString(), statement); err != nil {
		resp.Diagnostics.AddError("Create copy job", err.Error())
		return
	}

	data.ID = r.identity(data.Database.ValueString(), map[string]string{"name": data.Name.ValueString()})
	if data.Auto.IsUnknown() {
		data.Auto = types.BoolValue(true)
	}
	// A failed verification must still record the created job.
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)

	if err := r.verify(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Verify copy job", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Read refreshes the definition, AUTO setting and role, or removes a missing job.
func (r *copyJobResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data copyJobModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.read(ctx, &data)
	if err != nil {
		resp.Diagnostics.AddError("Read copy job", err.Error())
		return
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

// Update switches AUTO in place and verifies the job.
func (r *copyJobResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior copyJobModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)

	if resp.Diagnostics.HasError() {
		return
	}
	// Check ownership before any statement runs; verify would only notice after the ALTER.
	if err := r.bound(prior.ID, plan.Database.ValueString()); err != nil {
		resp.Diagnostics.AddError("Update copy job", err.Error())
		return
	}

	statements, err := alterCopyJobStatements(prior, plan)
	if err == nil {
		err = r.exec(ctx, plan.Database.ValueString(), statements...)
	}

	if err != nil {
		resp.Diagnostics.AddError("Update copy job", err.Error())
		return
	}

	plan.ID = prior.ID
	if err := r.verify(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Verify copy job", err.Error())
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete drops the job; data it loaded stays in the table.
func (r *copyJobResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data copyJobModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.read(ctx, &data)
	if err == nil && found {
		var statement string
		if statement, err = dropCopyJobStatement(data); err == nil {
			err = r.exec(ctx, data.Database.ValueString(), statement)
		}
	}

	if err != nil {
		resp.Diagnostics.AddError("Delete copy job", err.Error())
	}
}

// ImportState restores the identity; Read fills in the actual COPY definition, AUTO setting, and role.
func (r *copyJobResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "name")
}
