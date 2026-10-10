package provider

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// externalPartitionSpec is one partition with its values in the table's partition-key order.
type externalPartitionSpec struct {
	// schema and table name the partitioned external table.
	schema, table string
	// keys are the configured key spellings in partition-key order.
	keys []string
	// values are the partition values in the same order.
	values []string
	// location is the partition's S3 folder or manifest file.
	location string
}

// validateExternalPartitionValues checks a values map without the catalog: at least one key, nonempty names, and
// no names differing only in case, which the catalog cannot tell apart.
func validateExternalPartitionValues(values map[string]string) error {
	if len(values) == 0 {
		return fmt.Errorf("values must name at least one partition key")
	}
	seen := map[string]string{}
	for _, key := range slices.Sorted(maps.Keys(values)) {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("values need nonempty partition key names")
		}
		if previous, ok := seen[strings.ToLower(key)]; ok {
			return fmt.Errorf("values keys %q and %q name the same partition key", previous, key)
		}
		seen[strings.ToLower(key)] = key
	}
	return nil
}

// validateExternalPartitionConfig is the check ValidateConfig and Create share before the catalog is read.
func validateExternalPartitionConfig(data externalPartitionModel) error {
	if data.Schema.ValueString() == "" || data.Table.ValueString() == "" {
		return fmt.Errorf("external partition requires a nonempty schema and table")
	}
	if err := validateExternalPartitionValues(knownMap(data.Values)); err != nil {
		return err
	}
	return externalLocation(data.Location.ValueString())
}

// externalPartitionSpecFrom orders the configured values by the table's partition keys, which the catalog lists in
// key order. Every key must have exactly one value, because a partition is identified by all of them. The location
// is taken as is; Create and Update validate it, while Read may hold none yet after import.
func externalPartitionSpecFrom(data externalPartitionModel, partitionKeys []string) (externalPartitionSpec, error) {
	spec := externalPartitionSpec{schema: data.Schema.ValueString(), table: data.Table.ValueString(), location: data.Location.ValueString()}
	if spec.schema == "" || spec.table == "" {
		return spec, fmt.Errorf("external partition requires a nonempty schema and table")
	}
	values := knownMap(data.Values)
	if err := validateExternalPartitionValues(values); err != nil {
		return spec, err
	}
	if len(partitionKeys) == 0 {
		return spec, fmt.Errorf("external table %q has no partition keys", spec.table)
	}
	for _, key := range partitionKeys {
		found := false
		for configured, value := range values {
			if strings.EqualFold(configured, key) {
				spec.keys, spec.values, found = append(spec.keys, configured), append(spec.values, value), true
			}
		}
		if !found {
			return spec, fmt.Errorf("values must set every partition key of the table: %s", strings.Join(partitionKeys, ", "))
		}
	}
	if len(values) != len(partitionKeys) {
		return spec, fmt.Errorf("values must set only the partition keys of the table: %s", strings.Join(partitionKeys, ", "))
	}
	return spec, nil
}

// externalPartitionKeysMatch reports whether values set exactly the partition keys, compared without case.
func externalPartitionKeysMatch(values map[string]string, partitionKeys []string) bool {
	if len(values) != len(partitionKeys) || len(values) == 0 {
		return false
	}
	for _, key := range partitionKeys {
		found := false
		for configured := range values {
			found = found || strings.EqualFold(configured, key)
		}
		if !found {
			return false
		}
	}
	return true
}

// externalPartitionClause renders PARTITION ("key" = 'value', ...).
func externalPartitionClause(spec externalPartitionSpec) sqlclient.Statement {
	items := make([]sqlclient.Statement, len(spec.keys))
	for i, key := range spec.keys {
		items[i] = sqlclient.Ident(key).Kw("=").Lit(spec.values[i])
	}
	return sqlclient.Kw("PARTITION").Paren(items...)
}

// externalPartitionAlter starts every ALTER TABLE statement for the partition's table.
func externalPartitionAlter(spec externalPartitionSpec) sqlclient.Statement {
	return sqlclient.Stmt("ALTER TABLE").Append(externalTableRelation(spec.schema, spec.table))
}

// createExternalPartitionStatement renders ADD PARTITION without IF NOT EXISTS, so an existing partition fails
// creation instead of being adopted silently.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_TABLE.html
func createExternalPartitionStatement(spec externalPartitionSpec) string {
	return externalPartitionAlter(spec).Kw("ADD").Append(externalPartitionClause(spec)).KwLit("LOCATION", spec.location).String()
}

// externalPartitionAlterSteps change the partition in place; only its location can change.
var externalPartitionAlterSteps = []alterStep[externalPartitionSpec]{
	{
		attribute: "location",
		value:     func(spec externalPartitionSpec) attr.Value { return types.StringValue(spec.location) },
		render: func(_, plan externalPartitionSpec) []string {
			return []string{externalPartitionAlter(plan).Append(externalPartitionClause(plan)).KwLit("SET LOCATION", plan.location).String()}
		},
	},
}

// alterExternalPartitionStatements renders PARTITION (...) SET LOCATION when the location changes.
func alterExternalPartitionStatements(prev, plan externalPartitionSpec) []string {
	return alterStatements(prev, plan, externalPartitionAlterSteps)
}

// dropExternalPartitionStatement renders DROP PARTITION, which removes catalog metadata and leaves the S3 data.
func dropExternalPartitionStatement(spec externalPartitionSpec) string {
	return externalPartitionAlter(spec).Kw("DROP").Append(externalPartitionClause(spec)).String()
}

// readExternalPartitionsQuery lists the table's partitions. Matching happens in Go, because the view reports
// values as a JSON array whose exact spelling the reference does not pin down.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_EXTERNAL_PARTITIONS.html
func readExternalPartitionsQuery(spec externalPartitionSpec) sqlclient.Query {
	return sqlclient.Select("values", "location").
		From("svv_external_partitions").
		Where("LOWER(schemaname) = LOWER(:schema)", sqlclient.Bind("schema", spec.schema)).
		Where("LOWER(tablename) = LOWER(:table)", sqlclient.Bind("table", spec.table))
}

// externalPartitionValuesMatch reports whether a catalog values array holds the spec's values in key order.
func externalPartitionValuesMatch(spec externalPartitionSpec, catalog string) (bool, error) {
	var values []string
	if err := json.Unmarshal([]byte(catalog), &values); err != nil {
		return false, fmt.Errorf("decode external partition values %q: %w", catalog, err)
	}
	return slices.Equal(values, spec.values), nil
}

// externalPartitionIDValues encodes the values map for the JSON identity, whose fields must be strings.
func externalPartitionIDValues(values map[string]string) string {
	encoded, _ := json.Marshal(values) // String maps are always JSON-serializable, with sorted keys.
	return string(encoded)
}
