package provider

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// Identity provider types as svv_identity_providers.type reports them. r_CREATE_IDENTITY_PROVIDER: "Azure and AWSIDC
// are currently the only supported identity providers."
// https://docs.aws.amazon.com/redshift/latest/dg/r_CREATE_IDENTITY_PROVIDER.html
const (
	identityProviderAWSIDC = "awsidc"
	identityProviderAzure  = "azure"
)

// errIdentityProviderSecret rejects creating or re-parameterizing an Azure provider without the client secret.
// ALTER IDENTITY PROVIDER deletes every previously set parameter before assigning the new ones, so the secret must be
// sent with any parameter change, and the plan never carries write-only values.
var errIdentityProviderSecret = errors.New("client_secret_wo is required to create an azure identity provider and whenever issuer, client_id, audience, or client_secret_wo_version change, because PARAMETERS replaces every parameter")

// identityProviderType returns the configured type, defaulting to awsidc like the schema; state written before the
// attribute existed is null.
func identityProviderType(data identityProviderModel) string {
	if value := knownString(data.Type); value != "" {
		return strings.ToLower(value)
	}
	return identityProviderAWSIDC
}

// identityProviderAzureParameters is the PARAMETERS JSON of an Azure provider, in the order of the r_CREATE_IDENTITY_PROVIDER
// example.
type identityProviderAzureParameters struct {
	// Issuer is the token issuer URL of the Microsoft Entra ID tenant.
	Issuer string `json:"issuer"`
	// ClientID is the application (client) ID of the registered Redshift application.
	ClientID string `json:"client_id"`
	// ClientSecret is the application secret; the catalog never returns it.
	ClientSecret string `json:"client_secret"`
	// Audience lists the accepted token audiences.
	Audience []string `json:"audience,omitempty"`
}

// identityProviderAudience returns the configured audience sorted, so rendering and comparison ignore set order.
func identityProviderAudience(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted
}

// identityProviderParameters renders the Azure PARAMETERS object with secret.
func identityProviderParameters(data identityProviderModel, secret string) identityProviderAzureParameters {
	return identityProviderAzureParameters{Issuer: knownString(data.Issuer), ClientID: knownString(data.ClientID), ClientSecret: secret, Audience: identityProviderAudience(data.Audience)}
}

// identityProviderInputs holds the attributes that validateIdentityProviderInputs checks, by attribute name. Apart from
// type and auto_create_roles only their nullness matters, so the model, whose audience is a slice, and a
// configuration, whose audience set may be unknown, share one check.
type identityProviderInputs map[string]attr.Value

// identityProviderInputNames lists the attributes in identityProviderInputs.
var identityProviderInputNames = []string{
	"type", "application_arn", "iam_role_arn", "issuer", "client_id", "audience", "client_secret_wo", "client_secret_wo_version",
	"auto_create_roles", "auto_create_roles_include_groups", "auto_create_roles_exclude_groups",
}

// identityProviderModelInputs returns the validated attributes of a plan model.
func identityProviderModelInputs(data identityProviderModel) identityProviderInputs {
	audience := types.SetNull(types.StringType)
	if data.Audience != nil {
		audience = types.SetValueMust(types.StringType, nil)
	}
	return identityProviderInputs{
		"type": types.StringValue(identityProviderType(data)), "application_arn": data.ApplicationARN, "iam_role_arn": data.IAMRoleARN,
		"issuer": data.Issuer, "client_id": data.ClientID, "audience": audience, "client_secret_wo": data.ClientSecret,
		"client_secret_wo_version": data.ClientSecretVersion, "auto_create_roles": data.AutoCreateRoles,
		"auto_create_roles_include_groups": data.IncludeGroups, "auto_create_roles_exclude_groups": data.ExcludeGroups,
	}
}

// validateIdentityProvider rejects attribute combinations the provider type cannot use, before any SQL runs.
func validateIdentityProvider(data identityProviderModel) error {
	return validateIdentityProviderInputs(identityProviderModelInputs(data))
}

// validateIdentityProviderInputs rejects attribute combinations the provider type cannot use. APPLICATION_ARN and
// IAM_ROLE "are applicable only when the identity-provider type is AWSIDC", PARAMETERS carries the Azure issuer,
// client, audience, and secret, and the group filters qualify AUTO_CREATE_ROLES TRUE only. A check whose input is
// unknown is skipped, because the value may still turn out null or set. The secret version is required for Azure so
// that a null prior version can only come from an import, which identityProviderParametersChange relies on.
func validateIdentityProviderInputs(inputs identityProviderInputs) error {
	present := func(name string) bool { return !inputs[name].IsNull() && !inputs[name].IsUnknown() }
	if kind, ok := inputs["type"].(types.String); ok && !kind.IsUnknown() {
		name := identityProviderType(identityProviderModel{Type: kind})
		if _, err := sqlclient.OneOf(name, identityProviderAWSIDC, identityProviderAzure); err != nil {
			return fmt.Errorf("unsupported identity provider type: %w", err)
		}
		awsidc := []string{"application_arn", "iam_role_arn"}
		azure := []string{"issuer", "client_id", "client_secret_wo_version"}
		required, forbidden := awsidc, append(slices.Clone(azure), "audience", "client_secret_wo")
		if name == identityProviderAzure {
			required, forbidden = azure, awsidc
		}
		for _, attribute := range required {
			if inputs[attribute].IsNull() {
				return fmt.Errorf("type %s requires %s", name, attribute)
			}
		}
		for _, attribute := range forbidden {
			if present(attribute) {
				return fmt.Errorf("%s does not apply to type %s", attribute, name)
			}
		}
	}
	include, exclude := present("auto_create_roles_include_groups"), present("auto_create_roles_exclude_groups")
	if include && exclude {
		return errors.New("set at most one of auto_create_roles_include_groups and auto_create_roles_exclude_groups")
	}
	if autoCreate, ok := inputs["auto_create_roles"].(types.Bool); ok && (include || exclude) && !autoCreate.IsUnknown() && !autoCreate.ValueBool() {
		return errors.New("auto_create_roles_include_groups and auto_create_roles_exclude_groups require auto_create_roles = true")
	}
	return nil
}

// identityProviderValidated validates data together with the configured secret, which the plan never carries, so a
// secret that the type cannot use is rejected too.
func identityProviderValidated(data identityProviderModel, secret string) error {
	if secret != "" {
		data.ClientSecret = types.StringValue(secret)
	}
	return validateIdentityProvider(data)
}

// identityProviderAlter starts every ALTER IDENTITY PROVIDER statement for the provider.
func identityProviderAlter(data identityProviderModel) sqlclient.Statement {
	return sqlclient.Stmt("ALTER IDENTITY PROVIDER").Ident(data.Name.ValueString())
}

// identityProviderAutoCreate renders AUTO_CREATE_ROLES { TRUE [ { INCLUDE | EXCLUDE } GROUPS LIKE 'pattern' ] | FALSE }.
// An unset value renders nothing on creation, which keeps Redshift's type default, and that default on reset:
// FALSE for AWSIDC and TRUE for Azure.
func identityProviderAutoCreate(data identityProviderModel, reset bool) sqlclient.Statement {
	enabled := knownBool(data.AutoCreateRoles)
	if enabled == nil {
		if !reset {
			return sqlclient.Fragment()
		}
		fallback := identityProviderType(data) == identityProviderAzure
		enabled = &fallback
	}
	clause := sqlclient.Kw("AUTO_CREATE_ROLES").Toggle(*enabled, "TRUE", "FALSE")
	if !*enabled {
		return clause
	}
	return clause.OptLit("INCLUDE GROUPS LIKE", knownString(data.IncludeGroups)).OptLit("EXCLUDE GROUPS LIKE", knownString(data.ExcludeGroups))
}

// createIdentityProviderStatement renders CREATE IDENTITY PROVIDER in the clause order of r_CREATE_IDENTITY_PROVIDER.
// A new provider is enabled; identityProviderStatusStatement disables it separately because CREATE has no such option.
// secret is the configured Azure client secret, which the plan never carries.
func createIdentityProviderStatement(data identityProviderModel, secret string) (string, error) {
	if err := identityProviderValidated(data, secret); err != nil {
		return "", err
	}
	kind, err := sqlclient.OneOf(identityProviderType(data), "AWSIDC", "azure")
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("CREATE IDENTITY PROVIDER").Ident(data.Name.ValueString()).Kw("TYPE", kind).
		KwLit("NAMESPACE", data.Namespace.ValueString())
	if identityProviderType(data) == identityProviderAzure {
		if secret == "" {
			return "", errIdentityProviderSecret
		}
		statement = statement.Kw("PARAMETERS").JSON(identityProviderParameters(data, secret))
	} else {
		statement = statement.KwLit("APPLICATION_ARN", data.ApplicationARN.ValueString()).KwLit("IAM_ROLE", data.IAMRoleARN.ValueString())
	}
	statement = statement.Append(identityProviderAutoCreate(data, false))
	return statement.String(), statement.Err()
}

// identityProviderStatusStatement renders ENABLE or DISABLE.
func identityProviderStatusStatement(data identityProviderModel) string {
	return identityProviderAlter(data).Toggle(data.Enabled.ValueBool(), "ENABLE", "DISABLE").String()
}

// identityProviderRoleStatement renders the IAM_ROLE change.
func identityProviderRoleStatement(data identityProviderModel) string {
	return identityProviderAlter(data).KwLit("IAM_ROLE", data.IAMRoleARN.ValueString()).String()
}

// identityProviderParametersChange reports whether plan needs a new PARAMETERS object. A null prior secret version
// comes from import, which must not send a secret implicitly.
func identityProviderParametersChange(prev, plan identityProviderModel) bool {
	if identityProviderType(plan) != identityProviderAzure {
		return false
	}
	if !prev.Issuer.Equal(plan.Issuer) || !prev.ClientID.Equal(plan.ClientID) || !slices.Equal(identityProviderAudience(prev.Audience), identityProviderAudience(plan.Audience)) {
		return true
	}
	before, after := prev.ClientSecretVersion, plan.ClientSecretVersion
	return !before.IsNull() && !after.IsUnknown() && !before.Equal(after)
}

// identityProviderAlterSteps change the options that ALTER IDENTITY PROVIDER sets one per statement and that are
// applied only when they change. The IAM role and the status are reapplied on every update instead, and the Azure
// parameters are rendered together by identityProviderParametersChange, because PARAMETERS replaces all of them.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ALTER_IDENTITY_PROVIDER.html
var identityProviderAlterSteps = []alterStep[identityProviderModel]{
	{
		attribute: "namespace",
		value:     func(data identityProviderModel) attr.Value { return data.Namespace },
		render: func(_, plan identityProviderModel) []string {
			return []string{identityProviderAlter(plan).KwLit("NAMESPACE", plan.Namespace.ValueString()).String()}
		},
	},
	{
		// The filters are part of the AUTO_CREATE_ROLES clause, so one step compares the whole rendered clause.
		attribute: "auto_create_roles",
		value: func(data identityProviderModel) attr.Value {
			return types.StringValue(identityProviderAutoCreate(data, true).String())
		},
		render: func(_, plan identityProviderModel) []string {
			return []string{identityProviderAlter(plan).Append(identityProviderAutoCreate(plan, true)).String()}
		},
	},
}

// identityProviderAlterBatches renders the update from prev to plan in execution order, split so a refused statement
// names its setting: the AWSIDC IAM role, then namespace, parameters and role creation, then the status. secret is
// the configured client secret, required when the Azure parameters change.
func identityProviderAlterBatches(prev, plan identityProviderModel, secret string) (role, options, status []string, err error) {
	if err := identityProviderValidated(plan, secret); err != nil {
		return nil, nil, nil, err
	}
	if identityProviderType(plan) == identityProviderAWSIDC {
		role = []string{identityProviderRoleStatement(plan)}
	}
	options = alterStatements(prev, plan, identityProviderAlterSteps[:1])
	if identityProviderParametersChange(prev, plan) {
		if secret == "" {
			return nil, nil, nil, errIdentityProviderSecret
		}
		parameters := identityProviderAlter(plan).Kw("PARAMETERS").JSON(identityProviderParameters(plan, secret))
		if parameters.Err() != nil {
			return nil, nil, nil, parameters.Err()
		}
		options = append(options, parameters.String())
	}
	options = append(options, alterStatements(prev, plan, identityProviderAlterSteps[1:])...)
	return role, options, []string{identityProviderStatusStatement(plan)}, nil
}

// alterIdentityProviderStatements renders every update statement from prev to plan. The role and status are
// reapplied whatever the prior state, so an update also repairs drift that a refresh did not observe yet.
func alterIdentityProviderStatements(prev, plan identityProviderModel, secret string) ([]string, error) {
	role, options, status, err := identityProviderAlterBatches(prev, plan, secret)
	return slices.Concat(role, options, status), err
}

// dropIdentityProviderStatement renders DROP IDENTITY PROVIDER without CASCADE, which would drop the provider's users
// and roles.
func dropIdentityProviderStatement(data identityProviderModel) string {
	return sqlclient.Stmt("DROP IDENTITY PROVIDER").Ident(data.Name.ValueString()).String()
}

// readIdentityProviderQuery selects the catalog row of one identity provider; DESC IDENTITY PROVIDER returns the same
// columns, but only the view takes a bound name.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_IDENTITY_PROVIDERS.html
func readIdentityProviderQuery(data identityProviderModel) sqlclient.Query {
	return sqlclient.Select("name", "type", "instanceid", "namespc", "params", "enabled", "uid").From("svv_identity_providers").
		Where("name = :name", sqlclient.Bind("name", data.Name.ValueString()))
}

// readIdentityProviderUsersQuery finds federated users carrying the provider's namespace prefix. LEFT avoids
// LIKE, whose _ and % wildcards a namespace may contain.
func readIdentityProviderUsersQuery(data identityProviderModel) sqlclient.Query {
	return sqlclient.Select("usename").From("pg_user").
		Where("LEFT(usename, LENGTH(:prefix)) = :prefix", sqlclient.Bind("prefix", data.Namespace.ValueString()+":"))
}
