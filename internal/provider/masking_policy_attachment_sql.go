package provider

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// maskingAttachmentGranteeTypes are the recipients ATTACH MASKING POLICY accepts; groups cannot hold policies.
var maskingAttachmentGranteeTypes = []sqlclient.Keyword{"USER", "ROLE", "PUBLIC"}

// maskingAttachmentValidate checks the attachment tuple before any SQL runs.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ATTACH_MASKING_POLICY.html
func maskingAttachmentValidate(data maskingPolicyAttachmentModel) error {
	for _, field := range []struct {
		name  string
		value types.String
	}{{"database", data.Database}, {"policy", data.Policy}, {"schema", data.Schema}, {"relation", data.Relation}, {"grantee", data.Grantee}} {
		if knownString(field.value) == "" {
			return fmt.Errorf("%s must not be empty", field.name)
		}
	}
	if _, err := maskingAttachmentGrantee(data); err != nil {
		return err
	}
	if data.Columns.IsNull() {
		return fmt.Errorf("columns needs at least one column")
	}
	for _, field := range []struct {
		name string
		list types.List
	}{{"columns", data.Columns}, {"input_columns", data.InputColumns}} {
		if field.list.IsNull() || field.list.IsUnknown() {
			continue
		}
		names := maskingAttachmentNames(field.list)
		if len(names) == 0 {
			return fmt.Errorf("%s needs at least one column", field.name)
		}
		seen := map[string]bool{}
		for _, column := range names {
			// Redshift folds identifiers to lower case by default, so names differing only in case collide.
			key := strings.ToLower(column)
			if column == "" || seen[key] {
				return fmt.Errorf("%s must name distinct, nonempty columns", field.name)
			}
			seen[key] = true
		}
	}
	if knownInt64(data.Priority) != nil && data.Priority.ValueInt64() < 0 {
		return fmt.Errorf("priority must not be negative")
	}
	return nil
}

// maskingAttachmentNames returns the known strings of a column list in order.
func maskingAttachmentNames(list types.List) []string {
	if list.IsNull() || list.IsUnknown() {
		return nil
	}
	names := make([]string, 0, len(list.Elements()))
	for _, element := range list.Elements() {
		if text, ok := element.(types.String); ok && !text.IsNull() && !text.IsUnknown() {
			names = append(names, text.ValueString())
		}
	}
	return names
}

// maskingAttachmentNameList converts column names to a Terraform list.
func maskingAttachmentNameList(names []string) types.List {
	elements := make([]attr.Value, 0, len(names))
	for _, name := range names {
		elements = append(elements, types.StringValue(name))
	}
	return types.ListValueMust(types.StringType, elements)
}

// maskingAttachmentGrantee renders the recipient: a user name, ROLE "name", or PUBLIC, which must be named public.
func maskingAttachmentGrantee(data maskingPolicyAttachmentModel) (sqlclient.Statement, error) {
	kind, err := sqlclient.OneOf(data.GranteeType.ValueString(), maskingAttachmentGranteeTypes...)
	if err != nil {
		return sqlclient.Statement{}, fmt.Errorf("grantee_type: %w", err)
	}
	name := data.Grantee.ValueString()
	switch kind {
	case "USER":
		return sqlclient.Ident(name), nil
	case "ROLE":
		return sqlclient.Kw("ROLE").Ident(name), nil
	default:
		if name != "public" {
			return sqlclient.Statement{}, fmt.Errorf("PUBLIC requires grantee = public")
		}
		return sqlclient.Kw("PUBLIC"), nil
	}
}

// maskingAttachmentTarget renders policy ON "schema"."relation" (columns), the part ATTACH and DETACH share. The
// statements run in the policy's database, so policy and relation need no database qualifier.
func maskingAttachmentTarget(data maskingPolicyAttachmentModel) sqlclient.Statement {
	return sqlclient.Fragment().Ident(data.Policy.ValueString()).Kw("ON").Qualified(data.Schema.ValueString(), data.Relation.ValueString()).
		Paren(sqlclient.Fragment().Idents(maskingAttachmentNames(data.Columns)...))
}

// attachMaskingPolicyStatement renders ATTACH MASKING POLICY. USING is rendered only for configured input columns;
// without it Redshift feeds the masked columns themselves to the policy.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ATTACH_MASKING_POLICY.html
func attachMaskingPolicyStatement(data maskingPolicyAttachmentModel) (string, error) {
	if err := maskingAttachmentValidate(data); err != nil {
		return "", err
	}
	grantee, err := maskingAttachmentGrantee(data)
	if err != nil {
		return "", err
	}
	inputs := maskingAttachmentNames(data.InputColumns)
	statement := sqlclient.Stmt("ATTACH MASKING POLICY").Append(maskingAttachmentTarget(data)).
		When(len(inputs) > 0, func(s sqlclient.Statement) sqlclient.Statement {
			return s.Kw("USING").Paren(sqlclient.Fragment().Idents(inputs...))
		}).
		Kw("TO").Append(grantee).OptInt("PRIORITY", knownInt64(data.Priority))
	return statement.String(), statement.Err()
}

// detachMaskingPolicyStatement renders DETACH MASKING POLICY for the attachment's columns and recipient.
// https://docs.aws.amazon.com/redshift/latest/dg/r_DETACH_MASKING_POLICY.html
func detachMaskingPolicyStatement(data maskingPolicyAttachmentModel) (string, error) {
	grantee, err := maskingAttachmentGrantee(data)
	if err != nil {
		return "", err
	}
	statement := sqlclient.Stmt("DETACH MASKING POLICY").Append(maskingAttachmentTarget(data)).Kw("FROM").Append(grantee)
	return statement.String(), statement.Err()
}

// maskingAttachmentAlterSteps lists the in-place changes. Redshift documents no ALTER for an attachment, so a new
// priority detaches and re-attaches the policy; DETACH must come first because the same policy cannot be attached
// twice to one column and recipient. The update restores the previous attachment when the ATTACH fails.
var maskingAttachmentAlterSteps = []alterStep[maskingPolicyAttachmentModel]{{
	attribute: "priority",
	value:     func(m maskingPolicyAttachmentModel) attr.Value { return m.Priority },
	render: func(prev, plan maskingPolicyAttachmentModel) []string {
		// alterMaskingAttachmentStatements validated both tuples before the steps run.
		detach, _ := detachMaskingPolicyStatement(prev)
		attach, _ := attachMaskingPolicyStatement(plan)
		return []string{detach, attach}
	},
}}

// alterMaskingAttachmentStatements renders the change from the attachment the catalog holds to the planned one.
func alterMaskingAttachmentStatements(current, plan maskingPolicyAttachmentModel) ([]string, error) {
	if err := maskingAttachmentValidate(plan); err != nil {
		return nil, err
	}
	if _, err := detachMaskingPolicyStatement(current); err != nil {
		return nil, err
	}
	return alterStatements(current, plan, maskingAttachmentAlterSteps), nil
}

// readMaskingAttachmentQuery reads the attachments of the policy on the relation from SVV_ATTACHED_MASKING_POLICY,
// which covers the connected database; recipients and columns are matched after decoding the JSON column lists.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_ATTACHED_MASKING_POLICY.html
func readMaskingAttachmentQuery(data maskingPolicyAttachmentModel) sqlclient.Query {
	return sqlclient.Select("policy_name", "schema_name", "table_name", "grantee", "grantee_type", "priority", "input_columns", "output_columns").
		From("svv_attached_masking_policy").
		Where("policy_name = :policy", sqlclient.Bind("policy", data.Policy.ValueString())).
		Where("schema_name = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("table_name = :relation", sqlclient.Bind("relation", data.Relation.ValueString()))
}

// maskingAttachmentPeersQuery reads every policy attached to the relation, so an update can find another policy that
// already holds the planned priority on one of the columns before it detaches anything.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SVV_ATTACHED_MASKING_POLICY.html
func maskingAttachmentPeersQuery(data maskingPolicyAttachmentModel) sqlclient.Query {
	return sqlclient.Select("policy_name", "grantee", "grantee_type", "priority", "output_columns").
		From("svv_attached_masking_policy").
		Where("schema_name = :schema", sqlclient.Bind("schema", data.Schema.ValueString())).
		Where("table_name = :relation", sqlclient.Bind("relation", data.Relation.ValueString()))
}

// maskingAttachmentPriorityConflict reports another policy that holds the planned priority on one of the planned
// columns. ATTACH rejects that even for other recipients, and the update has already detached the policy by then,
// so the check must run first. The same policy may share a priority on a column with itself for other recipients.
// https://docs.aws.amazon.com/redshift/latest/dg/r_ATTACH_MASKING_POLICY.html
func maskingAttachmentPriorityConflict(plan maskingPolicyAttachmentModel, peers []sqlclient.Row) error {
	if knownInt64(plan.Priority) == nil {
		return nil
	}
	priority := plan.Priority.ValueInt64()
	columns := maskingAttachmentNames(plan.Columns)
	for _, row := range peers {
		if strings.EqualFold(row["policy_name"], plan.Policy.ValueString()) {
			continue
		}
		peerPriority, err := maskingAttachmentPriority(row["priority"])
		if err != nil {
			return err
		}
		if peerPriority != priority {
			continue
		}
		outputs, err := maskingAttachmentParseNames(row["output_columns"])
		if err != nil {
			return err
		}
		for _, column := range columns {
			for _, output := range outputs {
				if strings.EqualFold(column, output) {
					return fmt.Errorf("masking policy %q is attached to column %q with priority %d for %s %q; Redshift does not allow two policies with equal priority on one column, so choose another priority",
						row["policy_name"], output, priority, row["grantee_type"], row["grantee"])
				}
			}
		}
	}
	return nil
}

// maskingAttachmentParseNames decodes a catalog column list such as ["email"].
func maskingAttachmentParseNames(text string) ([]string, error) {
	var names []string
	if err := json.Unmarshal([]byte(text), &names); err != nil {
		return nil, fmt.Errorf("unexpected attached masking policy columns %q: %w", text, err)
	}
	return names, nil
}

// maskingAttachmentNamesMatch compares column names without case, as Redshift folds identifiers by default.
func maskingAttachmentNamesMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(a[i], b[i]) {
			return false
		}
	}
	return true
}

// maskingAttachmentRow selects the catalog row of the tuple's recipient and output columns, if any.
func maskingAttachmentRow(data maskingPolicyAttachmentModel, rows []sqlclient.Row) (sqlclient.Row, error) {
	kind := data.GranteeType.ValueString()
	columns := maskingAttachmentNames(data.Columns)
	for _, row := range rows {
		if !strings.EqualFold(row["grantee_type"], kind) || kind != "PUBLIC" && !strings.EqualFold(row["grantee"], data.Grantee.ValueString()) {
			continue
		}
		outputs, err := maskingAttachmentParseNames(row["output_columns"])
		if err != nil {
			return nil, err
		}
		if maskingAttachmentNamesMatch(outputs, columns) {
			return row, nil
		}
	}
	return nil, nil
}

// maskingAttachmentPriority parses the catalog priority.
func maskingAttachmentPriority(text string) (int64, error) {
	priority, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("unexpected masking policy priority %q", text)
	}
	return priority, nil
}

// maskingAttachmentIdentity returns the JSON identity fields. Columns are part of the identity, encoded as a JSON
// list inside the string-valued identity, because one recipient can hold the policy on several column sets.
func maskingAttachmentIdentity(data maskingPolicyAttachmentModel) map[string]string {
	columns, _ := json.Marshal(maskingAttachmentNames(data.Columns)) // String slices are always JSON-serializable.
	return map[string]string{
		"policy": data.Policy.ValueString(), "schema": data.Schema.ValueString(), "relation": data.Relation.ValueString(),
		"columns": string(columns), "grantee": data.Grantee.ValueString(), "grantee_type": data.GranteeType.ValueString(),
	}
}
