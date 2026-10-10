package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newProcedureDataSource)

// procedureLookupArguments turns the selector's input types into IN argument blocks, which select the same overload
// because only the IN and INOUT types form its signature.
func procedureLookupArguments(selector types.List) types.List {
	inputs := routineStrings(selector)
	arguments := make([]procedureArgumentModel, len(inputs))
	for i, input := range inputs {
		arguments[i] = procedureArgumentModel{Name: types.StringNull(), Mode: types.StringNull(), Type: types.StringValue(input)}
	}
	return procedureArgumentList(arguments)
}

// procedureObservedArguments reports every catalog argument, as an empty list rather than null for a procedure
// without arguments, so length() and for expressions work on the lookup as they do on the resource's blocks.
func procedureObservedArguments(arguments []procedureArgumentModel) types.List {
	if len(arguments) == 0 {
		return types.ListValueMust(types.ObjectType{AttrTypes: procedureArgumentTypes}, []attr.Value{})
	}
	return procedureArgumentList(arguments)
}

// newProcedureDataSource looks up one stored procedure overload by its schema, name, and input argument types.
// nonatomic and configuration stay null because the catalog does not report them.
func newProcedureDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "procedure", factory: newProcedureResource, computed: []string{"body", "nonatomic", "configuration"},
		identityFields: []string{"schema", "name", "signature"}, identityDatabase: "database", adjust: routineLookupIdentity,
		selectors: map[string]schema.Attribute{
			"arguments": schema.ListAttribute{
				ElementType: types.StringType, Optional: true,
				MarkdownDescription: "Ordered `IN` and `INOUT` argument types selecting the overload, at most 32, as `signature` and the import identity hold them; `OUT` arguments are not part of it. Another spelling of the same type, such as `int` for `integer`, selects the same overload, and modifiers such as `varchar(64)` are ignored. Omit for a procedure without input arguments.",
				// A null element would otherwise drop out of the signature and select a shorter overload.
				Validators: []validator.List{listvalidator.SizeAtMost(routineMaxArguments), listvalidator.NoNullValues()},
			},
		},
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			attributes := data.Attributes()
			model := procedureModel{
				Database: attributes["database"].(types.String), Schema: attributes["schema"].(types.String), Name: attributes["name"].(types.String),
				Arguments: procedureLookupArguments(attributes["arguments"].(types.List)), ID: types.StringNull(), Owner: types.StringNull(), Body: types.StringNull(),
				DefinitionFingerprint: types.StringNull(), Security: types.StringNull(), Nonatomic: types.BoolNull(), Configuration: types.MapNull(types.StringType),
			}
			r := &procedureResource{*client}
			row, found, err := r.observe(ctx, model)
			if err != nil || !found {
				return found, err
			}
			row.apply(&model, false)
			for name, value := range map[string]attr.Value{
				"argument": procedureObservedArguments(row.arguments), "signature": model.Signature, "body": model.Body,
				"definition_fingerprint": model.DefinitionFingerprint, "security": model.Security, "owner": model.Owner,
			} {
				lookupValue(data, name, value)
			}
			return true, nil
		},
	}), map[string]string{
		"argument":      "Every argument in order, including `OUT` arguments, as `SHOW PARAMETERS` reports it.",
		"argument.name": "Argument name; `null` when the catalog does not keep it, as for an unnamed argument.",
		"argument.mode": "`OUT` or `INOUT`, or `null` for an `IN` argument. `OUT` arguments are returned by `CALL` and are not part of the signature.",
		"argument.type": "Argument data type without length or precision, such as `integer` or `character varying`.",
	})
}
