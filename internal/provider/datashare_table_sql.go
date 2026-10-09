package provider

import (
	"fmt"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// datashareTableObject renders the qualified relation. Empty parts are rejected because Qualified omits an empty
// schema, which would resolve the table through the search path instead.
func datashareTableObject(data datashareTableModel) (sqlclient.Statement, error) {
	if data.Schema.ValueString() == "" || data.Table.ValueString() == "" {
		return sqlclient.Statement{}, fmt.Errorf("datashare table requires a nonempty schema and table")
	}
	return sqlclient.Fragment().Qualified(data.Schema.ValueString(), data.Table.ValueString()), nil
}

// createDatashareTableStatement adds one existing table or view to the share.
func createDatashareTableStatement(data datashareTableModel) (string, error) {
	object, err := datashareTableObject(data)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).Kw("ADD TABLE").Append(object).String(), nil
}

// dropDatashareTableStatement removes the relation from the share without touching the relation itself.
func dropDatashareTableStatement(data datashareTableModel) (string, error) {
	object, err := datashareTableObject(data)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Datashare.ValueString()).Kw("REMOVE TABLE").Append(object).String(), nil
}

// readDatashareTableQuery reads the relation member, which the catalog names schema.table without quoting.
func readDatashareTableQuery(data datashareTableModel) sqlclient.Query {
	return sqlclient.Select("object_name").
		From("svv_datashare_objects").
		Where("share_type = 'OUTBOUND'").
		Where("share_name = :share", sqlclient.Bind("share", data.Datashare.ValueString())).
		Where("object_name = :object", sqlclient.Bind("object", data.Schema.ValueString()+"."+data.Table.ValueString())).
		Where("object_type IN ('table', 'view', 'late binding view', 'materialized view')")
}
