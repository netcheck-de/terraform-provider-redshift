package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// rlsFakeColumns is the polatts the fake reports, matching the representative policy's WITH clause.
const rlsFakeColumns = `[{"colname":"region","type":"character varying(64)"}]`

// rlsFakePredicate is the representative policy's USING expression.
const rlsFakePredicate = "region = current_user"

// rlsFake emulates one RLS policy, one attachment, and one relation's row-level security settings.
type rlsFake struct {
	// policy records whether the policy exists.
	policy bool
	// predicate is the stored USING expression.
	predicate string
	// columns is the polatts JSON of the WITH clause, empty without one.
	columns string
	// alias is the polalias of the WITH clause.
	alias string
	// attached records the policy/relation/recipient attachment.
	attached bool
	// relation records whether the protected relation exists.
	relation bool
	// rlsOn is the relation's ROW LEVEL SECURITY switch.
	rlsOn bool
	// conjunction is the CHAR(3) conjunction type as svv_rls_relation pads it.
	conjunction string
	// datashare is the FOR DATASHARES switch.
	datashare bool
}

var _ = registerFakeFamily("rls", func() fakeFamily {
	return &rlsFake{columns: rlsFakeColumns, relation: true, conjunction: "and", datashare: true}
})

// populate makes the policy exist, attached to a relation with row-level security on.
func (f *rlsFake) populate() {
	f.policy, f.predicate, f.columns, f.attached, f.relation, f.rlsOn = true, rlsFakePredicate, rlsFakeColumns, true, true, true
}

// rlsFakeWith records the WITH clause of a CREATE RLS POLICY statement the way svv_rls_policy reports it, folding
// the quoted names as Redshift does by default. It handles the simple quoted names the tests use.
func rlsFakeWith(sql string) (columns, alias string, err error) {
	head, _, _ := strings.Cut(sql, " USING (")
	_, with, found := strings.Cut(head, " WITH (")
	if !found {
		return "", "", nil
	}
	list, aliased, hasAlias := strings.Cut(with, ") AS ")
	if hasAlias {
		alias = strings.ToLower(strings.ReplaceAll(strings.Trim(aliased, `"`), `""`, `"`))
	} else {
		list = strings.TrimSuffix(list, ")")
	}
	var parsed []rlsPolicyCatalogColumn
	for _, item := range strings.Split(list, `, "`) {
		name, dataType, ok := strings.Cut(strings.TrimPrefix(item, `"`), `" `)
		if !ok {
			return "", "", fmt.Errorf("fake RLS catalog cannot parse WITH item %q", item)
		}
		parsed = append(parsed, rlsPolicyCatalogColumn{Name: strings.ToLower(name), Type: dataType})
	}
	encoded, err := json.Marshal(parsed)
	return string(encoded), alias, err
}

// rlsFakeUsing extracts the USING expression the provider parenthesized at the end of the statement.
func rlsFakeUsing(sql string) (string, error) {
	_, using, found := strings.Cut(sql, " USING (")
	if !found || !strings.HasSuffix(using, ")") {
		return "", fmt.Errorf("fake RLS catalog cannot parse USING in %q", sql)
	}
	return strings.TrimSuffix(using, ")"), nil
}

// query answers the RLS catalog views and applies policy, attachment, and ROW LEVEL SECURITY statements.
func (f *rlsFake) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	var err error
	switch {
	case strings.HasPrefix(sql, "SELECT poldb, polname"):
		if !f.policy {
			return nil, true, nil
		}
		name := parameters["name"]
		if name == "" {
			name = "region_filter"
		}
		return []dataapi.Row{{"poldb": parameters["database"], "polname": name, "polalias": f.alias, "polatts": f.columns, "polqual": f.predicate}}, true, nil
	case strings.HasPrefix(sql, "CREATE RLS POLICY"):
		if f.policy {
			return nil, true, errors.New("policy already exists")
		}
		if f.columns, f.alias, err = rlsFakeWith(sql); err == nil {
			f.predicate, err = rlsFakeUsing(sql)
		}
		f.policy = err == nil
	case strings.HasPrefix(sql, "ALTER RLS POLICY"):
		if !f.policy {
			return nil, true, errors.New("policy does not exist")
		}
		f.predicate, err = rlsFakeUsing(sql)
	case strings.HasPrefix(sql, "DROP RLS POLICY"):
		if f.attached {
			return nil, true, errors.New("policy is attached to a relation")
		}
		f.policy = false
	case strings.HasPrefix(sql, "SELECT polname, relschema"):
		if f.policy && f.attached {
			return []dataapi.Row{{"polname": parameters["policy"], "relschema": parameters["schema"], "relname": parameters["relation"], "grantee": parameters["grantee"], "granteekind": parameters["kind"]}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "ATTACH RLS POLICY"):
		if !f.policy || !f.relation || f.attached {
			return nil, true, errors.New("policy or relation is missing, or the policy is already attached")
		}
		f.attached = true
	case strings.HasPrefix(sql, "DETACH RLS POLICY"):
		f.attached = false
	case strings.HasPrefix(sql, "SELECT c.relname FROM pg_class"):
		if f.relation {
			return []dataapi.Row{{"relname": parameters["relation"]}}, true, nil
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "SELECT is_rls_on"):
		if !f.relation {
			return nil, true, nil
		}
		return []dataapi.Row{{"is_rls_on": fmt.Sprint(f.rlsOn), "is_rls_datashare_on": fmt.Sprint(f.datashare), "rls_conjunction_type": fmt.Sprintf("%-3s", f.conjunction)}}, true, nil
	case strings.HasPrefix(sql, "ALTER TABLE") && strings.Contains(sql, " ROW LEVEL SECURITY "):
		if !f.relation {
			return nil, true, errors.New("relation does not exist")
		}
		_, clause, _ := strings.Cut(sql, " ROW LEVEL SECURITY ")
		on := strings.HasPrefix(clause, "ON")
		switch {
		case strings.HasSuffix(clause, " FOR DATASHARES"):
			f.datashare = on
		default:
			f.rlsOn = on
			if _, conjunction, found := strings.Cut(clause, " CONJUNCTION TYPE "); found {
				f.conjunction = strings.ToLower(conjunction)
			}
		}
	default:
		return nil, false, nil
	}
	return nil, true, err
}
