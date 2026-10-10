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

// listDatasharesQuery lists inbound and outbound shares, optionally narrowed to one type or name, in a stable order.
func listDatasharesQuery(shareType, name string) sqlclient.Query {
	return sqlclient.Select(datashareColumns...).
		From(datashareSource).
		OptEq("d.share_type", "share_type", shareType).
		OptEq("d.share_name", "name", name).
		OrderBy("d.share_type", "d.share_name")
}
