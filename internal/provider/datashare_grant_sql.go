package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// spec renders USAGE ON DATASHARE "share" for the validated consumer through the shared grant shape. VIA DATA
// CATALOG follows the account on both GRANT and REVOKE (r_GRANT, r_REVOKE), so it belongs to the grantee.
func (data datashareGrantModel) spec() (grantSpec, error) {
	consumerType, _, value, err := data.consumer()
	if err != nil {
		return grantSpec{}, err
	}

	return grantSpec{
		object:  sqlclient.Fragment().KwIdent("ON DATASHARE", data.Datashare.ValueString()),
		grantee: sqlclient.Fragment().KwLit(consumerType, value).If(data.ViaDataCatalog.ValueBool(), "VIA DATA CATALOG"),
	}, nil
}

// createDatashareGrantStatement grants share usage to the consumer account or namespace.
func createDatashareGrantStatement(data datashareGrantModel) (string, error) {
	spec, err := data.spec()
	if err != nil {
		return "", err
	}

	return spec.statement(true, "USAGE"), nil
}

// dropDatashareGrantStatement revokes share usage from only this consumer.
func dropDatashareGrantStatement(data datashareGrantModel) (string, error) {
	spec, err := data.spec()
	if err != nil {
		return "", err
	}

	return spec.statement(false, "USAGE"), nil
}

// readDatashareGrantQuery checks consumer usage. An account grant has no namespace, so it must not match a
// namespace grant within the same account. SVV_DATASHARE_CONSUMERS documents no column for the VIA DATA CATALOG
// form, so both account forms read the same row.
func readDatashareGrantQuery(data datashareGrantModel) (sqlclient.Query, error) {
	consumerType, _, value, err := data.consumer()
	if err != nil {
		return sqlclient.Query{}, err
	}

	return sqlclient.Select("consumer_account", "consumer_namespace").From("svv_datashare_consumers").
		Where("share_name = :share", sqlclient.Bind("share", data.Datashare.ValueString())).
		WhereEither(consumerType == datashareGrantNamespace, "consumer_namespace = :namespace", "consumer_account = :account AND NVL(consumer_namespace, '') = ''",
			sqlclient.Bind("namespace", value), sqlclient.Bind("account", value)), nil
}

// listDatashareGrantsQuery lists consumer usage of the namespace's outbound shares, optionally of one share. The
// consumer view has no database column, so the producer database comes from svv_datashares.
func listDatashareGrantsQuery(share string) sqlclient.Query {
	return sqlclient.Select("BTRIM(d.source_database) AS source_database", "c.share_name", "c.consumer_account", "c.consumer_namespace").
		From("svv_datashare_consumers c JOIN svv_datashares d ON d.share_name = c.share_name AND d.share_type = 'OUTBOUND'").
		OptEq("c.share_name", "share", share).
		OrderBy("c.share_name", "c.consumer_account", "c.consumer_namespace")
}
