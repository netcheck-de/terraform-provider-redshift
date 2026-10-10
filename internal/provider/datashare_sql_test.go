package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// TestDatashareSQL pins the datashare statements for both accessibility values and the outbound lookup.
func TestDatashareSQL(t *testing.T) {
	share := func(name string, public bool) datashareModel {
		return datashareModel{Database: types.StringValue("analytics"), Name: types.StringValue(name), PublicAccessible: types.BoolValue(public)}
	}
	built := func(query sqlclient.Query) func() (string, error) {
		return func() (string, error) {
			sql, _, err := query.Build()
			return sql, err
		}
	}
	checkSQL(t, "datashare", []sqlCase{
		{"create", func() string { return createDatashareStatement(share("producer", false)) }},
		{"create_public", func() string { return createDatashareStatement(share("producer", true)) }},
		{"create_quoted", func() string { return createDatashareStatement(share(`Odd"Producer`, false)) }},
		{"alter", func() string { return alterDatashareStatement(share("producer", false)) }},
		{"alter_public", func() string { return alterDatashareStatement(share("producer", true)) }},
		{"alter_quoted", func() string { return alterDatashareStatement(share(`Odd"Producer`, true)) }},
		{"drop", func() string { return dropDatashareStatement(share("producer", false)) }},
		{"drop_quoted", func() string { return dropDatashareStatement(share(`Odd"Producer`, false)) }},
		{"read", built(readDatashareQuery(share(`Odd"Producer`, false)))},
		{"read_empty_name", built(readDatashareQuery(share("", false)))},
	})
}

// TestDatashareListSQL pins the listing for every filter combination. Filter values are bound parameters, so a
// name with quotes and backslashes never reaches the SQL text.
func TestDatashareListSQL(t *testing.T) {
	built := func(shareType, name string) func() (string, error) {
		return func() (string, error) {
			sql, parameters, err := listDatasharesQuery(shareType, name).Build()
			if err == nil && name != "" && parameters["name"] != name {
				return "", fmt.Errorf("name bound as %q", parameters["name"])
			}
			return sql, err
		}
	}
	checkSQL(t, "datashares", []sqlCase{
		{"all", built("", "")},
		{"outbound", built("OUTBOUND", "")},
		{"named", built("", `Odd"Share\'s`)},
		{"inbound_named", built("INBOUND", "source")},
	})
}
