package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ = registerDataSource(newDatashareMembershipsDataSource)

// datashareMembershipKinds are the member kinds, in listing order.
var datashareMembershipKinds = []string{"FUNCTIONS", "TABLES"}

// newDatashareMembershipsDataSource reports, for every schema of the outbound shares in one producer database and
// each member kind, whether the share holds every current source object, as redshift_datashare_membership does.
func newDatashareMembershipsDataSource() datasource.DataSource {
	filters := datashareObjectFilters()
	filters["schema"] = discoveryFilter("Only this shared schema; all shared schemas when omitted.")

	return newCollectionDataSource(collectionSpec{
		name:          "datashare_memberships",
		databaseField: "database",
		description: "Lists, for every schema of the outbound datashares in one producer database and each kind, whether the share holds every current source object of that kind, without taking ownership or requiring a SQL superuser. " +
			"Compares `SVV_DATASHARE_OBJECTS` with `SVV_ALL_TABLES` and `PG_PROC_INFO` in a fixed number of queries. " +
			"A schema whose name contains a period is omitted, as is a kind with a source object whose name contains one, because the catalog does not quote qualification. " +
			"Only visible rows are compared, so restricted visibility can report `all = true` without proving complete coverage. " + datashareMemberScope,
		filters: filters,
		element: describedOutputs(collectionElement(newDatashareMembershipResource, "database", "datashare", "schema", "kind", "all"), map[string]string{
			"schema": "Shared schema whose members are compared.",
			"kind":   "`TABLES` compares visible tables and views; `FUNCTIONS` compares visible SQL UDF overloads.",
			"all":    "Whether every currently visible source object of the kind has a visible share membership; true for an empty source set.",
		}),
		list: func(ctx context.Context, client *resourceClient, filters types.Object) ([]map[string]attr.Value, error) {
			database, rows, err := listDatashareObjects(ctx, client, filters, datashareMembershipObjectTypes)
			if err != nil || len(rows) == 0 {
				return nil, err
			}

			selected := objectString(filters, "schema")
			// candidates maps kind and schema to the unqualified names of its source objects.
			candidates := map[string]map[string][]string{}

			for _, kind := range datashareMembershipKinds {
				candidates[kind] = map[string][]string{}

				sources, err := client.selectRows(ctx, database, listDatashareMembershipCandidatesQuery(kind, database, selected))
				if err != nil {
					return nil, err
				}

				for _, row := range sources {
					candidates[kind][row["schema_name"]] = append(candidates[kind][row["schema_name"]], row["member_name"])
				}
			}

			// members maps share and kind to the catalog names of its members.
			members := map[string]map[string][]string{}

			for _, row := range rows {
				kind := "TABLES"

				switch {
				case datashareListedSchema(row):
					continue
				case row["object_type"] == "function":
					kind = "FUNCTIONS"
				}

				share := strings.TrimSpace(row["share_name"])
				if members[share] == nil {
					members[share] = map[string][]string{}
				}

				members[share][kind] = append(members[share][kind], row["object_name"])
			}

			items := []map[string]attr.Value{}

			for _, row := range rows {
				schema := row["object_name"]
				if !datashareListedSchema(row) || strings.Contains(schema, ".") || (selected != "" && schema != selected) {
					continue
				}

				share := strings.TrimSpace(row["share_name"])

				for _, kind := range datashareMembershipKinds {
					wanted, err := datashareMembershipWanted(schema, candidates[kind][schema])
					if err != nil {
						// A source name containing a period cannot be matched against the unquoted member names.
						continue
					}

					complete, _ := datashareMembershipComplete(wanted, members[share][kind])
					items = append(items, map[string]attr.Value{
						"database": types.StringValue(database), "datashare": types.StringValue(share),
						"schema": types.StringValue(schema), "kind": types.StringValue(kind), "all": types.BoolValue(complete),
					})
				}
			}

			return items, nil
		},
	})
}
