package provider

import (
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// spec renders USAGE ON DATASHARE "share" for the validated consumer through the shared grant shape.
func (data datashareGrantModel) spec() (grantSpec, error) {
	consumerType, _, value, err := data.consumer()
	if err != nil {
		return grantSpec{}, err
	}
	return grantSpec{
		object:  sqlclient.Fragment().KwIdent("ON DATASHARE", data.Datashare.ValueString()),
		grantee: sqlclient.Fragment().KwLit(consumerType, value),
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
// namespace grant within the same account.
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
