package provider

import (
	"maps"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// policyGrantTupleFields is a masking policy's lookup table tuple.
var policyGrantTupleFields = map[string]string{"database_name": "analytics", "schema_name": "public", "object_name": "masking_exempt", "policy_type": "MASKING", "policy_name": "mask_email"}

// policyGrantCase renders the parent checks and GRANT/REVOKE of a tuple with the recipient override; there is no
// privilege read to render.
func policyGrantCase(t *testing.T, changes map[string]string) func() ([]string, error) {
	t.Helper()
	return func() ([]string, error) {
		fields := maps.Clone(policyGrantTupleFields)
		maps.Copy(fields, changes)
		r := newPolicyGrantResource().(*policyGrantResource)
		target, err := r.target(privilegeObject(t, r.privilegeResource, fields))
		if err != nil {
			return nil, err
		}
		statements := make([]string, 0, len(target.checks)+2)
		for _, check := range target.checks {
			statements = append(statements, check.sql)
		}
		return append(statements, target.grant.statement(true, "SELECT"), target.grant.statement(false, "SELECT")), nil
	}
}

// TestPolicyGrantSQL pins GRANT/REVOKE SELECT ON TABLE … TO/FROM { RLS | MASKING } POLICY against r_GRANT and
// r_REVOKE, with the policy and table existence checks.
func TestPolicyGrantSQL(t *testing.T) {
	checkSQL(t, "policy_grant", []sqlCase{
		{"masking", policyGrantCase(t, nil)},
		{"rls", policyGrantCase(t, map[string]string{"policy_type": "RLS", "policy_name": "policy_concerts"})},
		{"lowercase_type", policyGrantCase(t, map[string]string{"policy_type": "masking"})},
		{"quoted_identifiers", policyGrantCase(t, map[string]string{"database_name": `Odd"Database`, "schema_name": `Odd"Schema`, "object_name": `It's\Table`, "policy_name": `Odd"Policy`})},
		{"unsupported_type", policyGrantCase(t, map[string]string{"policy_type": "ROLE"})},
		{"empty_policy", policyGrantCase(t, map[string]string{"policy_name": ""})},
		{"empty_table", policyGrantCase(t, map[string]string{"object_name": ""})},
	})
}

// TestPolicyGrantRecipient renders both policy kinds and rejects other recipients on its own, since prepare
// normally rejects them first.
func TestPolicyGrantRecipient(t *testing.T) {
	r := newPolicyGrantResource().(*policyGrantResource).privilegeResource
	for _, test := range []struct {
		changes  map[string]string
		expected string
	}{
		{nil, `MASKING POLICY "mask_email"`},
		{map[string]string{"policy_type": "rls", "policy_name": `Odd"Policy`}, `RLS POLICY "Odd""Policy"`},
		{map[string]string{"policy_type": "ROLE"}, ""},
		{map[string]string{"policy_name": ""}, ""},
	} {
		fields := maps.Clone(policyGrantTupleFields)
		maps.Copy(fields, test.changes)
		recipient, err := policyGrantRecipient(privilegeObject(t, r, fields))
		if test.expected == "" {
			require.Error(t, err, "%v", test.changes)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, test.expected, recipient.String())
	}
}
