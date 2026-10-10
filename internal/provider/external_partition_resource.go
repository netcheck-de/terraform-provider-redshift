package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// externalPartitionResource manages one partition of a partitioned external table.
type externalPartitionResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// externalPartitionModel is the Terraform state of one partition.
type externalPartitionModel struct {
	// ID records the warehouse/database/schema/table/values identity.
	ID types.String `tfsdk:"id"`
	// Database is the local database holding the external schema.
	Database types.String `tfsdk:"database"`
	// Schema is the external schema of the table.
	Schema types.String `tfsdk:"schema"`
	// Table is the partitioned external table.
	Table types.String `tfsdk:"table"`
	// Values maps every partition key to the partition's value.
	Values types.Map `tfsdk:"values"`
	// Location is the partition's S3 folder or manifest file.
	Location types.String `tfsdk:"location"`
}

var _ = registerResource(newExternalPartitionResource)

// newExternalPartitionResource constructs an external partition lifecycle handler.
func newExternalPartitionResource() resource.Resource { return &externalPartitionResource{} }

// Metadata identifies the external partition resource to Terraform.
func (r *externalPartitionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_external_partition"
}

// Schema defines the partition identity and its in-place location.
func (r *externalPartitionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages one partition of a partitioned Redshift Spectrum external table with `ALTER TABLE ... ADD PARTITION`.",
		Attributes: map[string]schema.Attribute{
			"id":       idAttribute(),
			"database": schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Local Redshift database holding the external schema. Changing it replaces the partition."},
			"schema":   schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "External schema of the table. Changing it replaces the partition."},
			"table":    schema.StringAttribute{Required: true, PlanModifiers: replace, MarkdownDescription: "Partitioned external table, for example a `redshift_external_table`. Changing it replaces the partition."},
			"values": schema.MapAttribute{
				ElementType: types.StringType, Required: true, PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
				MarkdownDescription: "Partition value for every partition key of the table, keyed by partition key name, such as `{ saledate = \"2008-01\" }`. Statements list the keys in the table's partition-key order. Changing it replaces the partition.",
			},
			"location": schema.StringAttribute{Required: true, MarkdownDescription: "`s3://` folder or manifest file holding the partition's data. Changes run `ALTER TABLE ... PARTITION (...) SET LOCATION`."},
		},
	}
}

// ValidateConfig reports values or a location Create would reject, during planning once they are known.
func (r *externalPartitionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data externalPartitionModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() || !req.Config.Raw.IsFullyKnown() {
		return
	}
	if err := validateExternalPartitionConfig(data); err != nil {
		resp.Diagnostics.AddError("Invalid external partition", err.Error())
	}
}

// partitionKeys reads the table's partition keys in key order; an empty result means the table, its schema or its
// database is gone.
func (r *externalPartitionResource) partitionKeys(ctx context.Context, data externalPartitionModel) ([]string, error) {
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return nil, err
	}
	if exists, err := r.localDatabaseExists(ctx, data.Database.ValueString()); err != nil || !exists {
		return nil, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readExternalTableColumnsQuery(data.Database.ValueString(), data.Schema.ValueString(), data.Table.ValueString()))
	if err != nil {
		return nil, err
	}
	_, keys, err := externalTableCatalogColumns(rows)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = key.name
	}
	return names, nil
}

// read refreshes the partition location; found is false when the partition, its table, or a parent is gone, or
// when the table's partition keys no longer match the configured values.
func (r *externalPartitionResource) read(ctx context.Context, data *externalPartitionModel) (externalPartitionSpec, bool, error) {
	keys, err := r.partitionKeys(ctx, *data)
	if err != nil || len(keys) == 0 {
		return externalPartitionSpec{}, false, err
	}
	// Values that do not fit the table's current partition keys cannot name an existing partition.
	if !externalPartitionKeysMatch(knownMap(data.Values), keys) {
		return externalPartitionSpec{}, false, nil
	}
	spec, err := externalPartitionSpecFrom(*data, keys)
	if err != nil {
		return spec, false, err
	}
	rows, err := r.selectRows(ctx, data.Database.ValueString(), readExternalPartitionsQuery(spec))
	if err != nil {
		return spec, false, err
	}
	// The view has no database column, so an equally named external schema of another database can list the same
	// partition values. The same Glue table seen through both schemas reports the same location; different
	// locations mean different tables, and picking one could read or verify the wrong partition.
	var locations []string
	for _, row := range rows {
		match, err := externalPartitionValuesMatch(spec, row["values"])
		if err != nil {
			return spec, false, err
		}
		if match && !slices.Contains(locations, row["location"]) {
			locations = append(locations, row["location"])
		}
	}
	switch len(locations) {
	case 0:
		return spec, false, nil
	case 1:
	default:
		return spec, false, fmt.Errorf("external partition %s of table %q is ambiguous in the catalog: locations %s", externalPartitionIDValues(knownMap(data.Values)), data.Table.ValueString(), strings.Join(locations, ", "))
	}
	if location := knownString(data.Location); location == "" || !externalLocationsMatch(location, locations[0]) {
		data.Location = types.StringValue(locations[0])
	}
	spec.location = data.Location.ValueString()
	return spec, true, nil
}

// partitionIdentity encodes the partition identity; values is a JSON string because identity fields are strings.
func (r *externalPartitionResource) partitionIdentity(data externalPartitionModel) types.String {
	return r.identity(data.Database.ValueString(), map[string]string{"schema": data.Schema.ValueString(), "table": data.Table.ValueString(), "values": externalPartitionIDValues(knownMap(data.Values))})
}

// Create adds the partition after checking its values against the table's partition keys, and verifies it.
func (r *externalPartitionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data externalPartitionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := validateExternalPartitionConfig(data)
	var keys []string
	if err == nil {
		keys, err = r.partitionKeys(ctx, data)
	}
	var spec externalPartitionSpec
	if err == nil {
		spec, err = externalPartitionSpecFrom(data, keys)
	}
	if err == nil {
		err = r.exec(ctx, data.Database.ValueString(), createExternalPartitionStatement(spec))
	}
	if err != nil {
		resp.Diagnostics.AddError("Create external partition", err.Error())
		return
	}
	data.ID = r.partitionIdentity(data)
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	planned := data.Location
	_, found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Verify external partition", err.Error())
	case !found:
		resp.Diagnostics.AddError("Verify external partition", "The partition is absent after adding it.")
	case !data.Location.Equal(planned):
		resp.Diagnostics.AddError("Verify external partition", "The catalog reports location "+data.Location.ValueString()+" after adding the partition.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Read refreshes the location or removes a partition that is gone, with its table, schema or database.
func (r *externalPartitionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data externalPartitionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, found, err := r.read(ctx, &data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read external partition", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update moves the partition to the planned location and verifies it.
func (r *externalPartitionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, prior externalPartitionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	current, found, err := r.read(ctx, &prior)
	if err == nil && !found {
		err = fmt.Errorf("the partition disappeared; refresh the plan")
	}
	if err == nil {
		err = externalLocation(plan.Location.ValueString())
	}
	if err == nil {
		next := current
		next.location = plan.Location.ValueString()
		err = r.exec(ctx, plan.Database.ValueString(), alterExternalPartitionStatements(current, next)...)
	}
	if err != nil {
		resp.Diagnostics.AddError("Update external partition", err.Error())
		return
	}
	observed := plan
	_, found, err = r.read(ctx, &observed)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Verify external partition", err.Error())
	case !found:
		resp.Diagnostics.AddError("Verify external partition", "The partition disappeared during the update.")
	case !observed.Location.Equal(plan.Location):
		resp.Diagnostics.AddError("Verify external partition", "The catalog reports location "+observed.Location.ValueString()+" after the update.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &observed)...)
	}
}

// Delete drops the partition and verifies its removal; the S3 data is not touched.
func (r *externalPartitionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data externalPartitionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	spec, found, err := r.read(ctx, &data)
	if err == nil && found {
		err = r.exec(ctx, data.Database.ValueString(), dropExternalPartitionStatement(spec))
		if err == nil {
			_, found, err = r.read(ctx, &data)
			if err == nil && found {
				err = fmt.Errorf("the partition remains after deletion")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Delete external partition", err.Error())
	}
}

// ImportState restores the partition identity; the values field holds the values map encoded as a JSON string.
func (r *externalPartitionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importIdentity(ctx, req, resp, "database", "schema", "table")
	if resp.Diagnostics.HasError() {
		return
	}
	// importIdentity has already decoded the identity as a JSON object of strings.
	var fields, values map[string]string
	_ = json.Unmarshal([]byte(req.ID), &fields)
	err := json.Unmarshal([]byte(fields["values"]), &values)
	if err == nil {
		err = validateExternalPartitionValues(values)
	}
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identity", "Field values must be a JSON object string of partition key values: "+err.Error())
		return
	}
	elements := map[string]attr.Value{}
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("values"), types.MapValueMust(types.StringType, elements))...)
}
