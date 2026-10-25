package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// tableValidateKnown checks only facts whose dependencies are known. In particular, one unknown column name may
// satisfy an otherwise missing key reference, but cannot make a known invalid type or duplicate name valid.
func tableValidateKnown(data tableModel) error {
	ctx := context.Background()

	for label, value := range map[string]types.String{"schema": data.Schema, "name": data.Name} {
		if !value.IsUnknown() {
			if err := tableNonemptyName(label, value.ValueString()); err != nil {
				return err
			}
		}
	}

	if _, err := tableKeyword(data.Backup, "backup", tableBackupModes...); err != nil {
		return err
	}

	columns := map[string]tableColumnModel{}
	columnTypes := map[string]sqlclient.Keyword{}

	var models []tableColumnModel

	if data.Column.IsNull() {
		return fmt.Errorf("at least one column block is required")
	}

	allNames := !data.Column.IsUnknown()
	if allNames && (len(data.Column.Elements()) == 0 || len(data.Column.Elements()) > tableMaxColumns) {
		return fmt.Errorf("a table needs between 1 and %d columns", tableMaxColumns)
	}

	for _, element := range data.Column.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsUnknown() {
			allNames = false
			continue
		}

		var column tableColumnModel
		if object.As(ctx, &column, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid column block")
		}

		models = append(models, column)
		if err := tableValidateKnownColumn(column); err != nil {
			return err
		}

		if column.Name.IsUnknown() {
			allNames = false
			continue
		}

		name := column.Name.ValueString()
		if _, duplicate := columns[name]; duplicate {
			return fmt.Errorf("column %q is declared twice", name)
		}

		columns[name] = column
		if !column.Type.IsUnknown() {
			columnTypes[name], _ = sqlclient.ColumnType(column.Type.ValueString())
		}
	}

	sources := map[string]bool{}

	for _, column := range models {
		if column.RenamedFrom.IsNull() || column.RenamedFrom.IsUnknown() {
			continue
		}

		source := column.RenamedFrom.ValueString()

		_, declared := columns[source]
		if err := tableRenameSource(source, sources, declared); err != nil {
			return err
		}
	}

	checkNames := func(value types.List, what string, local, key bool) error {
		if value.IsUnknown() {
			return nil
		}

		if value.IsNull() || len(value.Elements()) == 0 {
			return fmt.Errorf("%s needs at least one column", what)
		}

		var names []string

		for _, element := range value.Elements() {
			name, ok := element.(types.String)
			if !ok || name.IsNull() {
				return fmt.Errorf("%s must list distinct nonempty names", what)
			}

			if name.IsUnknown() {
				continue
			}

			names = append(names, name.ValueString())
		}

		if err := tableDistinctNames(what, names); err != nil {
			return err
		}

		if err := tableFolded(what, names...); err != nil {
			return err
		}

		for _, text := range names {
			if _, exists := columns[text]; local && allNames && !exists {
				return fmt.Errorf("%s names column %q, which the table does not declare", what, text)
			}

			if key {
				if err := tableKeyType(what, text, columnTypes[text]); err != nil {
					return err
				}
			}
		}

		return nil
	}

	if !data.PrimaryKey.IsNull() && !data.PrimaryKey.IsUnknown() {
		var primary tableKeyModel
		if data.PrimaryKey.As(ctx, &primary, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid primary_key block")
		}

		if err := checkNames(primary.Columns, "primary_key.columns", true, false); err != nil {
			return err
		}

		for _, element := range primary.Columns.Elements() {
			name, ok := element.(types.String)
			if ok && !name.IsUnknown() && columns[name.ValueString()].Nullable.ValueBool() {
				return fmt.Errorf("primary key column %q cannot be nullable", name.ValueString())
			}
		}
	}

	for _, element := range data.Unique.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsUnknown() {
			continue
		}

		var unique tableKeyModel
		if object.As(ctx, &unique, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid unique block")
		}

		if err := checkNames(unique.Columns, "unique.columns", true, false); err != nil {
			return err
		}
	}

	for _, element := range data.ForeignKey.Elements() {
		object, ok := element.(types.Object)
		if !ok || object.IsUnknown() {
			continue
		}

		var foreign tableForeignKeyModel
		if object.As(ctx, &foreign, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid foreign_key block")
		}

		if err := checkNames(foreign.Columns, "foreign_key.columns", true, false); err != nil {
			return err
		}

		if foreign.References.IsUnknown() {
			continue
		}

		if foreign.References.IsNull() {
			return fmt.Errorf("a foreign key needs a references block")
		}

		var references tableReferencesModel
		if foreign.References.As(ctx, &references, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid foreign_key.references block")
		}

		for _, name := range []types.String{references.Schema, references.Table} {
			if name.IsUnknown() {
				continue
			}

			if name.ValueString() == "" {
				return fmt.Errorf("a foreign key needs a nonempty references schema and table")
			}

			if err := tableFolded("foreign_key reference", name.ValueString()); err != nil {
				return err
			}
		}

		if err := checkNames(references.Columns, "foreign_key.references.columns", false, false); err != nil {
			return err
		}

		if !foreign.Columns.IsUnknown() && !references.Columns.IsUnknown() && len(foreign.Columns.Elements()) != len(references.Columns.Elements()) {
			return fmt.Errorf("a foreign key needs as many references columns as columns")
		}
	}

	if !data.Distribution.IsNull() && !data.Distribution.IsUnknown() {
		var distribution tableDistributionModel
		if data.Distribution.As(ctx, &distribution, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid distribution block")
		}

		style, err := tableKeyword(distribution.Style, "distribution.style", tableDistStyles...)
		if err != nil {
			return err
		}

		if !distribution.Key.IsUnknown() {
			key := distribution.Key.ValueString()
			if !distribution.Style.IsUnknown() {
				if err := tableDistributionKey(style, key); err != nil {
					return err
				}
			}

			if key != "" {
				if err := checkNames(tableStringListValue([]string{key}), "distribution.key", true, true); err != nil {
					return err
				}
			}
		}
	}

	if !data.SortKey.IsNull() && !data.SortKey.IsUnknown() {
		var sort tableSortKeyModel
		if data.SortKey.As(ctx, &sort, basetypes.ObjectAsOptions{}).HasError() {
			return fmt.Errorf("invalid sort_key block")
		}

		style, err := tableKeyword(sort.Style, "sort_key.style", tableSortKeyStyles...)
		if err != nil {
			return err
		}

		if !sort.Columns.IsUnknown() {
			count := len(sort.Columns.Elements())
			if err := tableSortKeyCount(style, count); err != nil {
				return err
			}

			if count > 0 || !sort.Columns.IsNull() {
				if err := checkNames(sort.Columns, "sort_key.columns", true, true); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// tableValidateKnownColumn checks the known names of one column, skipping values not yet known.
func tableValidateKnownColumn(column tableColumnModel) error {
	for label, value := range map[string]types.String{"column name": column.Name, "renamed_from": column.RenamedFrom} {
		if value.IsUnknown() || (label == "renamed_from" && value.IsNull()) {
			continue
		}

		if err := tableNonemptyName(label, value.ValueString()); err != nil {
			return err
		}
	}

	var dataType sqlclient.Keyword

	if !column.Type.IsUnknown() {
		var err error
		if dataType, err = sqlclient.ColumnType(column.Type.ValueString()); err != nil {
			return err
		}
	}

	if _, err := tableKeyword(column.Encoding, "encoding", tableEncodings...); err != nil {
		return err
	}

	if !column.Default.IsUnknown() && !column.Default.IsNull() {
		if _, err := sqlclient.CheckUserSQL(column.Default.ValueString()); err != nil {
			return err
		}
	}

	if column.Identity.IsNull() || column.Identity.IsUnknown() {
		return nil
	}

	var identity tableIdentityModel
	if column.Identity.As(context.Background(), &identity, basetypes.ObjectAsOptions{}).HasError() {
		return fmt.Errorf("invalid identity block")
	}

	return tableValidateIdentity(column.Name.ValueString(), dataType, identity,
		!column.Default.IsNull() && !column.Default.IsUnknown(), column.Nullable.ValueBool())
}

// tableValidateIdentity checks an identity column's options; an empty type is not yet known and skips only the
// type check.
func tableValidateIdentity(name string, dataType sqlclient.Keyword, identity tableIdentityModel, hasDefault, nullable bool) error {
	switch {
	case dataType != "" && dataType != "INTEGER" && dataType != "BIGINT":
		return fmt.Errorf("identity column %q must be INTEGER or BIGINT", name)
	case identity.Seed.IsNull() || identity.Step.IsNull():
		return fmt.Errorf("identity column %q needs a seed and a step", name)
	case !identity.Step.IsUnknown() && identity.Step.ValueInt64() == 0:
		return fmt.Errorf("identity column %q needs a nonzero step", name)
	case hasDefault:
		return fmt.Errorf("identity column %q cannot also have a default", name)
	case nullable:
		return fmt.Errorf("identity column %q is always NOT NULL", name)
	}

	return nil
}

// tableNonemptyName rejects an empty name and one with uppercase letters, which Redshift folds to lowercase.
func tableNonemptyName(what, name string) error {
	if name == "" {
		return fmt.Errorf("%s must be nonempty", what)
	}

	return tableFolded(what, name)
}

// tableRenameSource rejects a renamed_from used twice or naming a configured column, which would make the rename
// order ambiguous.
func tableRenameSource(source string, seen map[string]bool, declared bool) error {
	if seen[source] {
		return fmt.Errorf("renamed_from %q is used more than once", source)
	}

	seen[source] = true
	if declared {
		return fmt.Errorf("renamed_from %q is also a configured column name; self-renames, chains, and swaps are not supported", source)
	}

	return nil
}

// tableDistributionKey pairs a distribution key with style KEY; an empty style is unset or not yet known and is
// resolved when the definition is rendered.
func tableDistributionKey(style sqlclient.Keyword, key string) error {
	switch {
	case style == "KEY" && key == "":
		return fmt.Errorf("distribution style KEY needs a key")
	case style != "" && style != "KEY" && key != "":
		return fmt.Errorf("distribution key requires style KEY")
	}

	return nil
}

// tableSortKeyCount checks the column count against the sort key style; an empty style is unset or not yet known.
func tableSortKeyCount(style sqlclient.Keyword, count int) error {
	switch {
	case count > tableMaxCompoundSortKeys:
		return fmt.Errorf("a sort key has at most %d columns", tableMaxCompoundSortKeys)
	case (style == "AUTO" || style == "NONE") && count > 0:
		return fmt.Errorf("sort key style %s takes no columns; remove sort_key.columns", style)
	case (style == "COMPOUND" || style == "INTERLEAVED") && count == 0:
		return fmt.Errorf("sort key style %s needs sort_key.columns", style)
	case style == "INTERLEAVED" && count > tableMaxInterleavedSortKey:
		return fmt.Errorf("an interleaved sort key has at most %d columns", tableMaxInterleavedSortKey)
	}

	return nil
}
