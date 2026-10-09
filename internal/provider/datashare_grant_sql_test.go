package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestDatashareGrantSQL pins the consumer read and GRANT/REVOKE USAGE for account and namespace consumers.
// Consumer values pass a strict pattern before rendering, so only the share name can carry quoting edge cases.
func TestDatashareGrantSQL(t *testing.T) {
	render := func(share, account, namespace string) func() ([]string, error) {
		data := datashareGrantModel{Database: types.StringValue("analytics"), Datashare: types.StringValue(share), AccountID: types.StringNull(), NamespaceID: types.StringNull()}
		if account != "" {
			data.AccountID = types.StringValue(account)
		}
		if namespace != "" {
			data.NamespaceID = types.StringValue(namespace)
		}
		return func() ([]string, error) {
			query, err := readDatashareGrantQuery(data)
			if err != nil {
				return nil, err
			}
			read, _, err := query.Build()
			if err != nil {
				return nil, err
			}
			create, err := createDatashareGrantStatement(data)
			if err != nil {
				return nil, err
			}
			drop, err := dropDatashareGrantStatement(data)
			return []string{read, create, drop}, err
		}
	}
	checkSQL(t, "datashare_grant", []sqlCase{
		{"account", render("producer", "123456789012", "")},
		{"namespace", render("producer", "", "12345678-1234-1234-1234-123456789abc")},
		{"quoted_share", render(`Odd"Share`, "123456789012", "")},
		{"invalid_account", render("producer", "1234'", "")},
		{"invalid_namespace", render("producer", "", `x\y`)},
		{"both_consumers", render("producer", "123456789012", "12345678-1234-1234-1234-123456789abc")},
		{"without_consumer", render("producer", "", "")},
		{"empty_share", render("", "123456789012", "")},
	})
}
