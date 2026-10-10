package provider

import (
	"fmt"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// rlsPolicyAttachmentGranteeTypes are the recipient forms of ATTACH RLS POLICY ... TO.
var rlsPolicyAttachmentGranteeTypes = []string{"USER", "ROLE", "PUBLIC"}

// rlsPolicyAttachmentParts renders the qualified relation and the recipient. Empty names are rejected because
// Qualified omits an empty schema, which would resolve the relation through the search path instead.
func rlsPolicyAttachmentParts(data rlsPolicyAttachmentModel) (sqlclient.Statement, sqlclient.Statement, error) {
	var relation, grantee sqlclient.Statement
	if data.Policy.ValueString() == "" || data.Schema.ValueString() == "" || data.Relation.ValueString() == "" {
		return relation, grantee, fmt.Errorf("RLS policy attachment requires a nonempty policy, schema, and relation")
	}
	relation = sqlclient.Fragment().Qualified(data.Schema.ValueString(), data.Relation.ValueString())
	name := data.Grantee.ValueString()
	switch kind := data.GranteeType.ValueString(); kind {
	case "USER":
		grantee = sqlclient.Ident(name)
	case "ROLE":
		grantee = sqlclient.Kw("ROLE").Ident(name)
	case "PUBLIC":
		if name != "public" {
			return relation, grantee, fmt.Errorf("grantee_type PUBLIC requires grantee = public")
		}
		return relation, sqlclient.Kw("PUBLIC"), nil
	default:
		return relation, grantee, fmt.Errorf("grantee_type %q is not one of %s", kind, strings.Join(rlsPolicyAttachmentGranteeTypes, ", "))
	}
	if name == "" {
		return relation, grantee, fmt.Errorf("RLS policy attachment requires a nonempty grantee")
	}
	return relation, grantee, nil
}

// createRlsPolicyAttachmentStatement renders ATTACH RLS POLICY for one relation and one recipient. Attaching
// does not turn row-level security on for the relation; redshift_table_security owns that switch.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ATTACH_RLS_POLICY.html
func createRlsPolicyAttachmentStatement(data rlsPolicyAttachmentModel) (string, error) {
	relation, grantee, err := rlsPolicyAttachmentParts(data)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("ATTACH RLS POLICY").Ident(data.Policy.ValueString()).Kw("ON").Append(relation).Kw("TO").Append(grantee).String(), nil
}

// dropRlsPolicyAttachmentStatement renders DETACH RLS POLICY for the same relation and recipient.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DETACH_RLS_POLICY.html
func dropRlsPolicyAttachmentStatement(data rlsPolicyAttachmentModel) (string, error) {
	relation, grantee, err := rlsPolicyAttachmentParts(data)
	if err != nil {
		return "", err
	}
	return sqlclient.Stmt("DETACH RLS POLICY").Ident(data.Policy.ValueString()).Kw("ON").Append(relation).Kw("FROM").Append(grantee).String(), nil
}

// readRlsPolicyAttachmentQuery reads one attachment in the connected database. The view reports user and role
// recipients with granteekind user or role; PUBLIC is matched by its grantee name alone.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_RLS_ATTACHED_POLICY.html
func readRlsPolicyAttachmentQuery(data rlsPolicyAttachmentModel) sqlclient.Query {
	kind := data.GranteeType.ValueString()
	return sqlclient.Select("polname", "relschema", "relname", "grantee", "granteekind").From("svv_rls_attached_policy").
		Where("polname = :policy", sqlclient.Bind("policy", data.Policy.ValueString())).
		Where("relschema = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("relname = :relation", sqlclient.Bind("relation", data.Relation.ValueString())).
		Where("grantee = :grantee", sqlclient.Bind("grantee", data.Grantee.ValueString())).
		WhereIf(kind != "PUBLIC", "granteekind = :kind", sqlclient.Bind("kind", strings.ToLower(kind)))
}
