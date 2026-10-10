package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newRoutineParametersDataSource)

// newRoutineParametersDataSource lists the parameters of matching functions and procedures. It reads the
// overloads from pg_proc_info and then runs SHOW PARAMETERS for each one, because the catalog keeps parameter
// modes and names in arrays that only SHOW PARAMETERS resolves to type names.
func newRoutineParametersDataSource() datasource.DataSource {
	filters := routineListingFilters("routine_name", "routines")
	filters["routine_type"] = schema.StringAttribute{
		Optional: true, MarkdownDescription: "Only routines of this type: `FUNCTION` or `PROCEDURE`.",
		Validators: []validator.String{stringvalidator.OneOf("FUNCTION", "PROCEDURE")},
	}
	element := map[string]schema.Attribute{
		"database":         schema.StringAttribute{Computed: true, MarkdownDescription: "Database containing the routine."},
		"schema":           schema.StringAttribute{Computed: true, MarkdownDescription: "Schema containing the routine."},
		"routine_name":     schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the function or procedure."},
		"routine_type":     schema.StringAttribute{Computed: true, MarkdownDescription: "`FUNCTION` or `PROCEDURE`."},
		"arguments":        schema.ListAttribute{ElementType: types.StringType, Computed: true, MarkdownDescription: "Input argument types that identify the routine's overload, as the catalog spells them."},
		"parameter_name":   schema.StringAttribute{Computed: true, MarkdownDescription: "Parameter name; empty for unnamed parameters and the `RETURN` row."},
		"ordinal_position": schema.Int64Attribute{Computed: true, MarkdownDescription: "Position of the parameter, starting at 1; `0` for a function's `RETURN` row."},
		"mode":             schema.StringAttribute{Computed: true, MarkdownDescription: "`IN`, `OUT`, `INOUT`, or `RETURN` for a function's result."},
		"data_type":        schema.StringAttribute{Computed: true, MarkdownDescription: "Parameter data type, as `SHOW PARAMETERS` reports it."},
	}
	return newCollectionDataSource(collectionSpec{
		name:        "routine_parameters",
		description: "Lists the parameters of user-defined functions and stored procedures in one database, one item per parameter and overload, without managing them. Narrow the listing with `schema` and `routine_name`, because each matching overload costs one `SHOW PARAMETERS` call.",
		filters:     filters,
		element:     element,
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, routines, err := routineListingRows(ctx, client, filters, routineListingTypes[objectString(filters, "routine_type")], "routine_name")
			if err != nil {
				return nil, err
			}
			var items []map[string]attr.Value
			for _, routine := range routines {
				kind, ok := routineListingKinds[routine["kind"]]
				if !ok {
					return nil, fmt.Errorf("routine %s.%s has unknown kind %q", routine["schema_name"], routine["routine_name"], routine["kind"])
				}
				statement, err := showRoutineParametersStatement(database, routine["schema_name"], routine["routine_name"], string(kind), routine["arguments"])
				if err != nil {
					return nil, fmt.Errorf("routine %s.%s: %w", routine["schema_name"], routine["routine_name"], err)
				}
				parameters, err := client.queryDatabase(ctx, database, statement, nil)
				if err != nil {
					return nil, err
				}
				for _, parameter := range parameters {
					position, err := routineListingInt(parameter, "ordinal_position")
					if err != nil {
						return nil, err
					}
					items = append(items, map[string]attr.Value{
						"database":         types.StringValue(database),
						"schema":           types.StringValue(routine["schema_name"]),
						"routine_name":     types.StringValue(routine["routine_name"]),
						"routine_type":     types.StringValue(string(kind)),
						"arguments":        externalFunctionTypeValues(externalFunctionSignatureTypes(routine["arguments"])),
						"parameter_name":   types.StringValue(parameter["parameter_name"]),
						"ordinal_position": types.Int64Value(position),
						"mode":             types.StringValue(parameter["parameter_type"]),
						"data_type":        types.StringValue(parameter["data_type"]),
					})
				}
			}
			return items, nil
		},
	})
}
