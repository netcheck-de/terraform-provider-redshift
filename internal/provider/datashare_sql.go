package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createDatashareStatement renders CREATE DATASHARE with an explicit accessibility, so the result never depends
// on the server default.
func createDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("CREATE DATASHARE").Ident(data.Name.ValueString()).Kw("SET PUBLICACCESSIBLE").Bool(data.PublicAccessible.ValueBool()).String()
}

// alterDatashareStatement sets accessibility, the only in-place setting. Update renders it even when the plan
// matches state, so an update also repairs drift that the preceding refresh did not observe.
func alterDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Name.ValueString()).Kw("SET PUBLICACCESSIBLE").Bool(data.PublicAccessible.ValueBool()).String()
}

// dropDatashareStatement renders DROP DATASHARE; members and consumers are separate resources removed first.
func dropDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("DROP DATASHARE").Ident(data.Name.ValueString()).String()
}

// readDatashareQuery reads the outbound share only, because an inbound share may carry the same name.
func readDatashareQuery(data datashareModel) sqlclient.Query {
	return sqlclient.Select("share_name", "share_type", "source_database", "is_publicaccessible", "managed_by").
		From("svv_datashares").
		Where("share_name = :name", sqlclient.Bind("name", data.Name.ValueString())).
		Where("share_type = 'OUTBOUND'")
}
