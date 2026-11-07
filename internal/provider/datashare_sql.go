package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// createDatashareStatement renders CREATE DATASHARE with an explicit accessibility, so the result never depends
// on the server default. The issuer becomes the owner (r_CREATE_DATASHARE), which read reports.
func createDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("CREATE DATASHARE").Ident(data.Name.ValueString()).Kw("SET PUBLICACCESSIBLE").Bool(data.PublicAccessible.ValueBool()).String()
}

// alterDatashareStatement sets accessibility, the only in-place setting: ALTER DATASHARE documents no OWNER TO
// clause, so the owner is observed only. Update renders it even when the plan matches state, so an update also
// repairs drift that the preceding refresh did not observe.
func alterDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("ALTER DATASHARE").Ident(data.Name.ValueString()).Kw("SET PUBLICACCESSIBLE").Bool(data.PublicAccessible.ValueBool()).String()
}

// dropDatashareStatement renders DROP DATASHARE; members and consumers are separate resources removed first.
func dropDatashareStatement(data datashareModel) string {
	return sqlclient.Stmt("DROP DATASHARE").Ident(data.Name.ValueString()).String()
}

// datashareColumns is the SVV_DATASHARES projection shared by the resource read and the listing. share_owner is a
// user ID (r_SVV_DATASHARES), so the owner name comes from pg_user; an inbound share has no local owner and keeps a
// null name through the outer join. createdate is cast to text so both transports report the same string.
var datashareColumns = []sqlclient.Keyword{
	"d.share_name", "d.share_type", "d.source_database", "d.consumer_database", "d.is_publicaccessible", "d.managed_by",
	"d.share_id", "u.usename AS owner", "d.producer_account", "d.producer_namespace", "CAST(d.createdate AS VARCHAR) AS created_at",
}

// datashareSource joins each share to its owning user.
const datashareSource sqlclient.Keyword = "svv_datashares d LEFT JOIN pg_user u ON u.usesysid = d.share_owner"

// readDatashareQuery reads the outbound share only, because an inbound share may carry the same name.
func readDatashareQuery(data datashareModel) sqlclient.Query {
	return sqlclient.Select(datashareColumns...).
		From(datashareSource).
		Where("d.share_name = :name", sqlclient.Bind("name", data.Name.ValueString())).
		Where("d.share_type = 'OUTBOUND'")
}

// listDatasharesQuery lists the outbound or inbound shares of the namespace, optionally narrowed to one name, in
// name order.
func listDatasharesQuery(outbound bool, name string) sqlclient.Query {
	return sqlclient.Select(datashareColumns...).
		From(datashareSource).
		WhereEither(outbound, "d.share_type = 'OUTBOUND'", "d.share_type = 'INBOUND'").
		OptEq("d.share_name", "name", name).
		OrderBy("d.share_name")
}

// datashareRelationTypes are the svv_datashare_objects types that ALTER DATASHARE ADD TABLE and ADD ALL TABLES share.
const datashareRelationTypes sqlclient.Keyword = "object_type IN ('table', 'view', 'late binding view', 'materialized view')"

// datashareSchemaTypes are the svv_datashare_objects spellings the catalog uses for a shared schema.
const datashareSchemaTypes sqlclient.Keyword = "object_type IN ('schema', 'schemas')"

// listDatashareObjectsQuery lists the members of the outbound shares of one producer database, optionally of one
// share, matching the given object type condition. svv_datashare_objects has no database column, so the share
// names come from svv_datashares, whose documented samples trim source_database.
func listDatashareObjectsQuery(database, share string, objectTypes sqlclient.Keyword) sqlclient.Query {
	return sqlclient.Select("share_name", "object_type", "object_name", "include_new").From("svv_datashare_objects").
		Where("share_type = 'OUTBOUND'").
		Where(objectTypes).
		Where("share_name IN (SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND BTRIM(source_database) = :database)", sqlclient.Bind("database", database)).
		OptEq("share_name", "share", share).
		OrderBy("share_name", "object_name")
}
