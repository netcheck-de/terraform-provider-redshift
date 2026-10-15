package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// identityProviderFakeFamily emulates svv_identity_providers for both provider types. The legacy identity, enabled,
// and iamRole fields keep their meaning, so existing tests that set them still describe an AWSIDC provider.
type identityProviderFakeFamily struct {
	// azure records an Azure provider instead of AWSIDC.
	azure bool
	// namespace is the provider namespace; empty reports the legacy "example".
	namespace string
	// parameters is the Azure catalog parameter text, with the secret redacted the way the view prints it.
	parameters string
	// secret is the last client secret sent, which the catalog never returns.
	secret string
	// autoCreate is the last AUTO_CREATE_ROLES clause, which the catalog does not report either.
	autoCreate string
	// federated lists the federated user names, which keep their prefix when the namespace changes.
	federated []string
}

var _ = registerFakeFamily("identity_provider", func() fakeFamily { return &identityProviderFakeFamily{} })

// query answers the provider read and applies CREATE, ALTER, and DROP IDENTITY PROVIDER.
func (f *identityProviderFakeFamily) query(c *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT name, type, instanceid, namespc, params, enabled, uid FROM svv_identity_providers"):
		if !c.identity {
			return nil, true, nil
		}
		namespace := f.namespace
		if namespace == "" {
			namespace = "example"
		}
		row := dataapi.Row{"name": parameters["name"], "type": "awsidc", "instanceid": "application", "namespc": namespace, "enabled": fmt.Sprint(c.enabled), "uid": "126692",
			"params": fmt.Sprintf(`{"iam_role":%q,"instance_arn":"arn:aws:sso:::instance/ssoins-1234567890abcdef","is_lakehouse_app":"false"}`, c.iamRole)}
		if f.azure {
			row["type"], row["instanceid"], row["params"] = "azure", "e40d4bb2-7670-44ae-bfb8-5db013221d73", f.parameters
		}
		return []dataapi.Row{row}, true, nil
	case strings.HasPrefix(sql, "SELECT usename FROM pg_user WHERE LEFT(usename"):
		var rows []dataapi.Row
		for _, user := range f.federated {
			if strings.HasPrefix(user, parameters["prefix"]) {
				rows = append(rows, dataapi.Row{"usename": user})
			}
		}
		return rows, true, nil
	case strings.HasPrefix(sql, "CREATE IDENTITY PROVIDER"):
		if c.identity {
			return nil, true, errors.New("identity provider already exists")
		}
		namespace, err := literal(sql, 0)
		if err != nil {
			return nil, true, err
		}
		f.namespace, f.azure, f.autoCreate = commentUnescaper.Replace(namespace), strings.Contains(sql, " TYPE AZURE "), ""
		if f.azure {
			if err := f.setParameters(sql); err != nil {
				return nil, true, err
			}
		} else {
			role, err := literal(sql, 2)
			if err != nil {
				return nil, true, err
			}
			c.iamRole = commentUnescaper.Replace(role)
		}
		if _, clause, ok := strings.Cut(sql, " AUTO_CREATE_ROLES "); ok {
			f.autoCreate = clause
		}
		c.identity, c.enabled = true, true
	case strings.HasPrefix(sql, "ALTER IDENTITY PROVIDER"):
		if !c.identity {
			return nil, true, errors.New("identity provider is absent")
		}
		switch {
		case strings.Contains(sql, " IAM_ROLE "):
			role, err := literal(sql, 0)
			if err != nil {
				return nil, true, err
			}
			c.iamRole = commentUnescaper.Replace(role)
		case strings.Contains(sql, " NAMESPACE "):
			namespace, err := literal(sql, 0)
			if err != nil {
				return nil, true, err
			}
			f.namespace = commentUnescaper.Replace(namespace)
		case strings.Contains(sql, " PARAMETERS "):
			if !f.azure {
				return nil, true, errors.New("only the fake Azure provider takes PARAMETERS")
			}
			if err := f.setParameters(sql); err != nil {
				return nil, true, err
			}
		case strings.Contains(sql, " AUTO_CREATE_ROLES "):
			_, f.autoCreate, _ = strings.Cut(sql, " AUTO_CREATE_ROLES ")
		case strings.HasSuffix(sql, " ENABLE"), strings.HasSuffix(sql, " DISABLE"):
			c.enabled = strings.HasSuffix(sql, " ENABLE")
		default:
			return nil, true, errors.New("unsupported ALTER IDENTITY PROVIDER: " + sql)
		}
	case strings.HasPrefix(sql, "DROP IDENTITY PROVIDER"):
		if c.role {
			return nil, true, errors.New("role dependency still exists")
		}
		c.identity = false
	default:
		return nil, false, nil
	}
	return nil, true, nil
}

// setParameters stores the PARAMETERS JSON the way svv_identity_providers prints it, without the secret.
func (f *identityProviderFakeFamily) setParameters(sql string) error {
	_, rest, _ := strings.Cut(sql, " PARAMETERS ")
	text, err := literal(rest, 0)
	if err != nil {
		return err
	}
	var sent identityProviderAzureParameters
	if err := json.Unmarshal([]byte(commentUnescaper.Replace(text)), &sent); err != nil {
		return fmt.Errorf("fake catalog cannot decode PARAMETERS: %w", err)
	}
	if sent.ClientSecret == "" {
		return errors.New("azure parameters need a client secret")
	}
	audience, err := json.Marshal(sent.Audience)
	if err != nil {
		return err
	}
	f.secret = sent.ClientSecret
	f.parameters = fmt.Sprintf(`{"issuer":%q, "client_id":%q, "client_secret":, "audience":%s}`, sent.Issuer, sent.ClientID, audience)
	return nil
}

// populate keeps the legacy AWSIDC provider that fullCatalog describes with its identity flags.
func (f *identityProviderFakeFamily) populate() {}
