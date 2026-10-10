package provider

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// maskingFakeFamily names the masking family in every fake catalog.
const maskingFakeFamily = "masking"

var _ = registerFakeFamily(maskingFakeFamily, func() fakeFamily { return &maskingFake{} })

// maskingFakeIdentifier matches a quoted identifier, keeping doubled quotes.
var maskingFakeIdentifier = regexp.MustCompile(`"((?:[^"]|"")*)"`)

// maskingFakePriority matches the trailing PRIORITY clause of ATTACH MASKING POLICY.
var maskingFakePriority = regexp.MustCompile(` PRIORITY (\d+)$`)

// maskingFake emulates one masking policy and one attachment of it, as SVV_MASKING_POLICY and
// SVV_ATTACHED_MASKING_POLICY report them.
type maskingFake struct {
	// policy records whether the masking policy exists.
	policy bool
	// name is the policy name.
	name string
	// expression is the stored masking expression, reported in the documented JSON form.
	expression string
	// attached records whether the policy is attached to the example column and role.
	attached bool
	// priority is the attachment priority.
	priority int64
}

// populate makes the representative policy and its attachment exist.
func (f *maskingFake) populate() {
	f.policy, f.name, f.expression, f.attached, f.priority = true, "mask_email", "CAST('***' AS TEXT)", true, 10
}

// maskingFakeName returns the first quoted identifier of sql without its quotes.
func maskingFakeName(sql string) string {
	match := maskingFakeIdentifier.FindStringSubmatch(sql)
	if match == nil {
		return ""
	}
	return strings.ReplaceAll(match[1], `""`, `"`)
}

// maskingFakeExpression returns the text inside USING ( … ) of a CREATE or ALTER statement.
func maskingFakeExpression(sql string) string {
	_, rest, _ := strings.Cut(sql, " USING (")
	return strings.TrimSuffix(rest, ")")
}

// query answers masking catalog reads and applies masking DDL; other SQL passes to the next family.
func (f *maskingFake) query(_ *catalog, connection dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT ") && strings.Contains(sql, " FROM svv_masking_policy"):
		if !f.policy || parameters["name"] != "" && parameters["name"] != f.name {
			return nil, true, nil
		}
		database := parameters["database"]
		if database == "" {
			database = connection.Database
		}
		expression, _ := json.Marshal([]map[string]string{{"expr": f.expression, "type": "character varying(256)"}})
		return []dataapi.Row{{"policy_database": database, "policy_name": f.name, "input_columns": `[{"colname":"email","type":"character varying(256)"}]`, "policy_expression": string(expression)}}, true, nil
	case strings.HasPrefix(sql, "SELECT ") && strings.Contains(sql, " FROM svv_attached_masking_policy"):
		if !f.policy || !f.attached || parameters["policy"] != f.name {
			return nil, true, nil
		}
		return []dataapi.Row{{"policy_name": f.name, "schema_name": parameters["schema"], "table_name": parameters["relation"], "grantee": "example_readers", "grantee_type": "role", "priority": strconv.FormatInt(f.priority, 10), "input_columns": `["email"]`, "output_columns": `["email"]`}}, true, nil
	case strings.HasPrefix(sql, "CREATE MASKING POLICY "):
		if f.policy {
			return nil, true, errors.New("masking policy already exists")
		}
		f.policy, f.name, f.expression = true, maskingFakeName(sql), maskingFakeExpression(sql)
	case strings.HasPrefix(sql, "ALTER MASKING POLICY "):
		if !f.policy {
			return nil, true, errors.New("masking policy does not exist")
		}
		f.expression = maskingFakeExpression(sql)
	case strings.HasPrefix(sql, "DROP MASKING POLICY "):
		if !f.policy || f.attached {
			return nil, true, errors.New("masking policy is absent or still attached")
		}
		f.policy = false
	case strings.HasPrefix(sql, "ATTACH MASKING POLICY "):
		if !f.policy || f.attached {
			return nil, true, errors.New("masking policy is absent or already attached")
		}
		f.attached, f.priority = true, 0
		if match := maskingFakePriority.FindStringSubmatch(sql); match != nil {
			f.priority, _ = strconv.ParseInt(match[1], 10, 64)
		}
	case strings.HasPrefix(sql, "DETACH MASKING POLICY "):
		if !f.attached {
			return nil, true, errors.New("masking policy is not attached")
		}
		f.attached = false
	default:
		return nil, false, nil
	}
	return nil, true, nil
}
