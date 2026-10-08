package provider

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAssumeroleGrantLifecycle exercises per-role IAM command reconciliation.
func TestAssumeroleGrantLifecycle(t *testing.T) {
	exercisePrivilege(t, newAssumeroleGrantResource, map[string]string{"iam_role_arn": "default", "grantee_type": "ROLE", "grantee": "readers"}, []string{"COPY", "UNLOAD"})
}

// TestAssumeroleGrantIAMRoleValidation checks ARN/default selection and grantee validation.
func TestAssumeroleGrantIAMRoleValidation(t *testing.T) {
	r := newAssumeroleGrantResource().(*privilegeResource)
	for _, role := range []string{"arn:aws:iam::123456789012:role/spectrum", "invalid", "arn:aws:s3:::bucket"} {
		_, err := r.prepare(privilegeObject(t, r, map[string]string{"iam_role_arn": role, "grantee_type": "USER", "grantee": "reader"}))
		if role == "arn:aws:iam::123456789012:role/spectrum" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	_, err := r.prepare(privilegeObject(t, r, map[string]string{"grantee_type": "invalid"}))
	require.Error(t, err)
}
