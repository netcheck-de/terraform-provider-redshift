package provider

import "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"

// listDatashareObjectsQuery lists share members of one database. Names remain opaque: AWS documents schema
// qualification but not a function overload encoding.
func listDatashareObjectsQuery(database, share, objectType string) sqlclient.Query {
	query := sqlclient.Select("share_name", "object_name", "object_type", "include_new").From("svv_datashare_objects").
		Where("share_type = 'OUTBOUND'").
		Where("share_name IN (SELECT share_name FROM svv_datashares WHERE share_type = 'OUTBOUND' AND source_database = :database)", sqlclient.Bind("database", database)).
		OptEq("share_name", "share", share)
	if objectType != "" {
		query = query.Where("object_type = :object_type", sqlclient.Bind("object_type", objectType))
	}
	return query.OrderBy("share_name", "object_type", "object_name")
}
