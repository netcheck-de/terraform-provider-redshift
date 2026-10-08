package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// datashareGrantResource manages SQL usage of an outbound share.
type datashareGrantResource struct {
	// resourceClient provides SQL execution and warehouse ownership checks.
	resourceClient
}

// datashareGrantModel is the Terraform state for a share/consumer grant.
type datashareGrantModel struct {
	// ID records producer database/share/consumer identity.
	ID types.String `tfsdk:"id"`
	// Database owns the producer share.
	Database types.String `tfsdk:"database"`
	// Datashare is the share whose usage is granted.
	Datashare types.String `tfsdk:"datashare"`
	// AccountID identifies the consumer AWS account.
	AccountID types.String `tfsdk:"account_id"`
	// NamespaceID identifies the consumer Redshift namespace UUID.
	NamespaceID types.String `tfsdk:"namespace_id"`
}

// datashareAccountPattern validates AWS account identifiers.
var datashareAccountPattern = regexp.MustCompile(`^[0-9]{12}$`)

// datashareNamespacePattern validates Redshift namespace UUIDs.
var datashareNamespacePattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// consumer validates the resolved selector and returns its SQL type and identity key.
func (data datashareGrantModel) consumer() (string, string, string, error) {
	if data.AccountID.IsUnknown() || data.NamespaceID.IsUnknown() {
		return "", "", "", fmt.Errorf("consumer identity must be known before executing SQL")
	}
	account, namespace := data.AccountID.ValueString(), data.NamespaceID.ValueString()
	if data.AccountID.IsNull() == data.NamespaceID.IsNull() {
		return "", "", "", fmt.Errorf("specify exactly one of account_id and namespace_id")
	}
	if account != "" {
		if !datashareAccountPattern.MatchString(account) {
			return "", "", "", fmt.Errorf("account_id must contain exactly 12 digits")
		}
		return "ACCOUNT", "account_id", account, nil
	}
	if !datashareNamespacePattern.MatchString(namespace) {
		return "", "", "", fmt.Errorf("namespace_id must be a UUID")
	}
	return "NAMESPACE", "namespace_id", namespace, nil
}

// newDatashareGrantResource constructs a SQL consumer share grant handler.
func newDatashareGrantResource() resource.Resource { return &datashareGrantResource{} }

// Metadata identifies the datashare grant resource to Terraform.
func (r *datashareGrantResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_datashare_grant"
}

// Schema defines share identity and exactly one receiving account or namespace.
func (r *datashareGrantResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Grants producer-side datashare USAGE to one consumer AWS account or Redshift namespace.",
		Attributes: map[string]schema.Attribute{
			"id":           idAttribute(),
			"database":     schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Local producer database owning the datashare."},
			"datashare":    schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Datashare name."},
			"account_id":   schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.ExactlyOneOf(path.MatchRoot("namespace_id")), stringvalidator.RegexMatches(datashareAccountPattern, "must contain exactly 12 digits")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Consumer AWS account ID. Specify exactly one of account_id and namespace_id."},
			"namespace_id": schema.StringAttribute{Optional: true, Validators: []validator.String{stringvalidator.RegexMatches(datashareNamespacePattern, "must be a namespace UUID")}, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}, MarkdownDescription: "Consumer Redshift namespace UUID, not an ARN. Specify exactly one of account_id and namespace_id."},
		},
	}
}

// read checks explicitly scoped consumer share usage in the producer catalog.
func (r *datashareGrantResource) read(ctx context.Context, data datashareGrantModel) (bool, error) {
	consumerType, _, value, err := data.consumer()
	if err != nil {
		return false, err
	}
	if err := r.bound(data.ID, data.Database.ValueString()); err != nil {
		return false, err
	}
	predicate := "consumer_account = :account AND NVL(consumer_namespace, '') = ''"
	parameters := map[string]string{"share": data.Datashare.ValueString(), "account": value}
	if consumerType == "NAMESPACE" {
		predicate = "consumer_namespace = :namespace"
		parameters = map[string]string{"share": data.Datashare.ValueString(), "namespace": value}
	}
	rows, err := r.queryDatabase(ctx, data.Database.ValueString(),
		"SELECT consumer_account, consumer_namespace FROM svv_datashare_consumers WHERE share_name = :share AND "+predicate, parameters)
	return len(rows) > 0, err
}

// Create grants consumer usage; AWS authorization and association remain separate.
func (r *datashareGrantResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data datashareGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	consumerType, field, value, err := data.consumer()
	if err != nil {
		resp.Diagnostics.AddError("Invalid datashare consumer", err.Error())
		return
	}
	sql := "GRANT USAGE ON DATASHARE " + sqlclient.Identifier(data.Datashare.ValueString()) + " TO " + consumerType + " " + sqlclient.Literal(value)
	if _, err := r.queryDatabase(ctx, data.Database.ValueString(), sql, nil); err != nil {
		resp.Diagnostics.AddError("Grant datashare usage", err.Error())
		return
	}
	data.ID = r.identity(data.Database.ValueString(), map[string]string{"datashare": data.Datashare.ValueString(), field: value})
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	found, err := r.read(ctx, data)
	if err != nil {
		resp.Diagnostics.AddError("Verify datashare grant", err.Error())
	} else if !found {
		resp.Diagnostics.AddError("Verify datashare grant", "The consumer grant is absent after creation.")
	}
}

// Read refreshes consumer usage and removes missing grants from state.
func (r *datashareGrantResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data datashareGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare grant", err.Error())
	case !found:
		resp.State.RemoveResource(ctx)
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Update verifies the immutable share/consumer grant remains present.
func (r *datashareGrantResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data datashareGrantModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	switch {
	case err != nil:
		resp.Diagnostics.AddError("Read datashare grant", err.Error())
	case !found:
		resp.Diagnostics.AddError("Update datashare grant", "The consumer grant disappeared; refresh the plan.")
	default:
		resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
	}
}

// Delete revokes only this consumer's SQL share usage and verifies removal.
func (r *datashareGrantResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data datashareGrantModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, data)
	if err == nil && found {
		consumerType, _, value, _ := data.consumer()
		sql := "REVOKE USAGE ON DATASHARE " + sqlclient.Identifier(data.Datashare.ValueString()) + " FROM " + consumerType + " " + sqlclient.Literal(value)
		_, err = r.queryDatabase(ctx, data.Database.ValueString(), sql, nil)
		if err == nil {
			found, err = r.read(ctx, data)
			if err == nil && found {
				resp.Diagnostics.AddError("Revoke datashare usage", "The consumer grant remains after revoking.")
			}
		}
	}
	if err != nil {
		resp.Diagnostics.AddError("Revoke datashare usage", err.Error())
	}
}

// ImportState restores producer database/share/consumer ownership.
func (r *datashareGrantResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	var values map[string]string
	if err := json.Unmarshal([]byte(req.ID), &values); err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	data := datashareGrantModel{AccountID: types.StringNull(), NamespaceID: types.StringNull()}
	if value, ok := values["account_id"]; ok {
		data.AccountID = types.StringValue(value)
	}
	if value, ok := values["namespace_id"]; ok {
		data.NamespaceID = types.StringValue(value)
	}
	_, field, _, err := data.consumer()
	if err != nil {
		resp.Diagnostics.AddError("Invalid import identity", err.Error())
		return
	}
	importIdentity(ctx, req, resp, "database", "datashare", field)
}
