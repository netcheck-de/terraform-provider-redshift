package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ = registerDataSource(newTableDataSource)

// newTableDataSource reads a table definition with the resource's catalog read, so lookup and resource report the
// same values.
func newTableDataSource() datasource.DataSource {
	return lookupDescriptions(newCatalogDataSource(catalogSpec{
		name: "table", factory: newTableResource,
		identityFields: []string{"schema", "name"}, identityDatabase: "database",
		lookup: func(ctx context.Context, client *resourceClient, data *types.Object) (bool, error) {
			var selected tableModel
			if diagnostics := data.As(ctx, &selected, basetypes.ObjectAsOptions{}); diagnostics.HasError() {
				return false, fmt.Errorf("read table lookup configuration: %v", diagnostics)
			}
			// Nothing but the selectors may shape the result, so the lookup reports catalog spellings. The empty
			// layout objects make read report the declared distribution and sort key even when they are AUTO,
			// which the resource leaves out while its blocks are omitted.
			observed := tableNullModel(selected.Database, selected.Schema, selected.Name)
			observed.Distribution = types.ObjectValueMust(tableDistributionAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "key": types.StringNull()})
			observed.SortKey = types.ObjectValueMust(tableSortKeyAttributeTypes, map[string]attr.Value{"style": types.StringNull(), "columns": types.ListNull(types.StringType)})
			found, _, err := (&tableResource{*client}).read(ctx, &observed)
			if err != nil || !found {
				return found, err
			}
			value, diagnostics := types.ObjectValueFrom(ctx, data.AttributeTypes(ctx), observed)
			if diagnostics.HasError() {
				return false, fmt.Errorf("convert table lookup: %v", diagnostics)
			}
			*data = value
			return true, nil
		},
	}), map[string]string{
		"column":       "Columns in physical order, as `pg_attribute` and `SVV_REDSHIFT_COLUMNS` report them.",
		"primary_key":  "Primary key; null without one.",
		"unique":       "UNIQUE constraints.",
		"foreign_key":  "FOREIGN KEY constraints.",
		"distribution": "Declared distribution, always reported: `style` is `AUTO`, `EVEN`, `KEY`, or `ALL`, and `key` is set for `KEY`. See `effective_distribution` for what Redshift applies under `AUTO`.",
		"sort_key": "Declared sort key, always reported: `style` is `AUTO`, `COMPOUND`, `INTERLEAVED`, or `NONE`, and `columns` is set for an explicit key. " +
			"When `SVV_TABLE_INFO` does not list the table, because it is empty or the provider's SQL identity may not see it, a key Redshift chose is reported as `COMPOUND` and a table without sort key columns as `AUTO`.",
		"backup": "Always null: whether snapshots include the table is not observable in a catalog every deployment exposes.",
		"owner":  "User owning the table.",
		"effective_distribution": "Distribution Redshift currently applies, from `PG_CLASS_INFO.releffectivediststyle` and the distribution key column in `SVV_REDSHIFT_COLUMNS`; " +
			"under `AUTO` it shows what Redshift picked, for example `{ style = \"KEY\", key = \"account_id\", auto = true }`.",
		"effective_sort_key": "Sort key Redshift currently applies, from the sort key positions in `SVV_REDSHIFT_COLUMNS` and `SVV_TABLE_INFO.sortkey1`; " +
			"under `AUTO` it shows the key Redshift chose, for example `{ style = \"COMPOUND\", columns = [\"created_at\"], auto = true }`.",
	})
}
