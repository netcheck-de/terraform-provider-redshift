package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestGrantRestoreSQL pins the statements that restore Redshift's PUBLIC defaults for languages and default function
// privileges on destroy.
func TestGrantRestoreSQL(t *testing.T) {
	var cases []sqlCase
	for _, test := range []struct {
		name    string
		factory func() resource.Resource
		fields  map[string]string
	}{
		{"sql", newLanguageGrantResource, map[string]string{"database_name": "warehouse", "language_name": "SQL", "grantee": "public", "grantee_type": "PUBLIC"}},
		{"plpgsql", newLanguageGrantResource, map[string]string{"database_name": "warehouse", "language_name": "PLPGSQL", "grantee": "public", "grantee_type": "PUBLIC"}},
		{"functions_owner", newDefaultPrivilegesResource, map[string]string{"database_name": "warehouse", "owner": "loader", "object_type": "FUNCTIONS", "grantee": "public", "grantee_type": "PUBLIC"}},
		{"functions_current_user", newDefaultPrivilegesResource, map[string]string{"database_name": "warehouse", "object_type": "FUNCTIONS", "grantee": "public", "grantee_type": "PUBLIC"}},
	} {
		cases = append(cases, sqlCase{name: test.name, render: func() ([]string, error) {
			r := test.factory().(*privilegeResource)
			data := privilegeObject(t, r, test.fields)
			lookupValue(&data, "restore_default_on_destroy", types.BoolValue(true))

			desired, err := r.destroyPrivileges(data)
			if err != nil {
				return nil, err
			}

			target, err := r.target(data)
			if err != nil {
				return nil, err
			}

			return privilegeOptionStatements(target.grant, target.allowed, privilegeSets{}, privilegeSets{privileges: desired})
		}})
	}

	checkSQL(t, "grant_restore", cases)
}
