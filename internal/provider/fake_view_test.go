package provider

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

var _ = registerFakeFamily("view", func() fakeFamily { return &viewFamily{relations: map[string]*fakeViewRelation{}} })

// fakeViewRelation is one view or materialized view in the fake catalog.
type fakeViewRelation struct {
	// schema and name identify the relation as the catalog reports them.
	schema, name string
	// query is the definition body after AS.
	query string
	// owner is the pg_views owner.
	owner string
	// lateBinding records WITH NO SCHEMA BINDING.
	lateBinding bool
	// materialized marks a materialized view.
	materialized bool
	// autoRefresh is the SVV_MV_INFO setting of a materialized view.
	autoRefresh bool
	// storage lists the ALTER DISTSTYLE and ALTER SORTKEY clauses applied to a materialized view, in order.
	storage []string
}

// definition mimics pg_get_viewdef: Redshift re-renders an ordinary view's SELECT and prints the complete CREATE
// statement of late-binding and materialized views.
func (v *fakeViewRelation) definition() string {
	relation := sqlclient.Identifier(v.schema) + "." + sqlclient.Identifier(v.name)
	switch {
	case v.materialized:
		return "create materialized view " + relation + " as " + v.query + ";"
	case v.lateBinding:
		return "create view " + relation + " as " + v.query + " with no schema binding;"
	default:
		return " " + strings.Join(strings.Fields(v.query), "\n") + ";"
	}
}

// viewFamily emulates views and materialized views keyed by schema and name.
type viewFamily struct {
	// relations maps schema.name to the relation.
	relations map[string]*fakeViewRelation
}

// Representative relations that fullCatalog populates and the lifecycle cases manage.
const (
	fakeViewSchema           = "serving"
	fakeViewName             = "sales_view"
	fakeMaterializedViewName = "sales_summary"
	fakeViewOwner            = "analyst"
	fakeViewQuery            = "SELECT id, label FROM serving.sales"
	fakeMaterializedQuery    = "SELECT label, COUNT(*) AS sales FROM serving.sales GROUP BY label"
)

// fakeViewKey joins the unquoted schema and name.
func fakeViewKey(schema, name string) string {
	return schema + "\x00" + name
}

// populate adds the representative view and materialized view.
func (f *viewFamily) populate() {
	f.relations[fakeViewKey(fakeViewSchema, fakeViewName)] = &fakeViewRelation{schema: fakeViewSchema, name: fakeViewName, query: fakeViewQuery, owner: fakeViewOwner}
	f.relations[fakeViewKey(fakeViewSchema, fakeMaterializedViewName)] = &fakeViewRelation{schema: fakeViewSchema, name: fakeMaterializedViewName, query: fakeMaterializedQuery, owner: fakeViewOwner, materialized: true, autoRefresh: true}
}

// remove deletes a relation, as an outside DROP would.
func (f *viewFamily) remove(name string) {
	delete(f.relations, fakeViewKey(fakeViewSchema, name))
}

// get returns a relation for test setup and assertions.
func (f *viewFamily) get(name string) *fakeViewRelation {
	return f.relations[fakeViewKey(fakeViewSchema, name)]
}

// fakeViewRelationPattern matches a leading "schema"."name" pair with doubled-quote escapes.
var fakeViewRelationPattern = regexp.MustCompile(`^"((?:[^"]|"")*)"\."((?:[^"]|"")*)"\s*`)

// fakeMaterializedViewStoragePattern matches the documented ALTER MATERIALIZED VIEW storage clauses the resource
// renders.
var fakeMaterializedViewStoragePattern = regexp.MustCompile(`^ALTER (DISTSTYLE (ALL|EVEN|KEY DISTKEY "(?:[^"]|"")+")|COMPOUND SORTKEY \("(?:[^"]|"")+"(, "(?:[^"]|"")+")*\)|SORTKEY NONE)$`)

// fakeViewTarget parses the relation after a statement prefix and returns the remaining text.
func fakeViewTarget(rest string) (schema, name, tail string, err error) {
	match := fakeViewRelationPattern.FindStringSubmatch(rest)
	if match == nil {
		return "", "", "", fmt.Errorf("fake view catalog cannot parse relation in %q", rest)
	}
	unquote := func(text string) string { return strings.ReplaceAll(text, `""`, `"`) }
	return unquote(match[1]), unquote(match[2]), rest[len(match[0]):], nil
}

// query applies view DDL and answers pg_views and SVV_MV_INFO reads.
func (f *viewFamily) query(_ *catalog, _ sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, bool, error) {
	switch {
	case strings.HasPrefix(sql, "SELECT schemaname, viewname, viewowner, definition FROM pg_views"):
		relation := f.relations[fakeViewKey(parameters["schema"], parameters["name"])]
		if relation == nil {
			return nil, true, nil
		}
		return []sqlclient.Row{{"schemaname": relation.schema, "viewname": relation.name, "viewowner": relation.owner, "definition": relation.definition()}}, true, nil
	case strings.HasPrefix(sql, "SELECT autorefresh FROM svv_mv_info"):
		relation := f.relations[fakeViewKey(parameters["schema"], parameters["name"])]
		if relation == nil || !relation.materialized {
			return nil, true, nil
		}
		return []sqlclient.Row{{"autorefresh": map[bool]string{true: "t", false: "f"}[relation.autoRefresh]}}, true, nil
	case strings.HasPrefix(sql, "CREATE VIEW "), strings.HasPrefix(sql, "CREATE OR REPLACE VIEW "):
		replace := strings.HasPrefix(sql, "CREATE OR REPLACE VIEW ")
		schema, name, tail, err := fakeViewTarget(strings.TrimPrefix(strings.TrimPrefix(sql, "CREATE OR REPLACE VIEW "), "CREATE VIEW "))
		if err != nil {
			return nil, true, err
		}
		existing := f.relations[fakeViewKey(schema, name)]
		switch {
		case existing != nil && (!replace || existing.materialized):
			return nil, true, fmt.Errorf("relation %q already exists", name)
		case !strings.HasPrefix(tail, "AS "):
			return nil, true, fmt.Errorf("fake view catalog expected AS in %q", sql)
		}
		body, late := strings.CutSuffix(strings.TrimPrefix(tail, "AS "), " WITH NO SCHEMA BINDING")
		relation := &fakeViewRelation{schema: schema, name: name, query: body, owner: "admin", lateBinding: late}
		if existing != nil {
			// CREATE OR REPLACE VIEW keeps ownership and grants.
			relation.owner = existing.owner
		}
		f.relations[fakeViewKey(schema, name)] = relation
		return nil, true, nil
	case strings.HasPrefix(sql, "CREATE MATERIALIZED VIEW "):
		schema, name, tail, err := fakeViewTarget(strings.TrimPrefix(sql, "CREATE MATERIALIZED VIEW "))
		if err != nil {
			return nil, true, err
		}
		if f.relations[fakeViewKey(schema, name)] != nil {
			return nil, true, fmt.Errorf("relation %q already exists", name)
		}
		options, body, found := strings.Cut(tail, "AS ")
		if !found {
			return nil, true, fmt.Errorf("fake view catalog expected AS in %q", sql)
		}
		f.relations[fakeViewKey(schema, name)] = &fakeViewRelation{schema: schema, name: name, query: body, owner: "admin", materialized: true, autoRefresh: strings.Contains(options, "AUTO REFRESH YES")}
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER TABLE "):
		var relation *fakeViewRelation
		schema, name, tail, parseErr := fakeViewTarget(strings.TrimPrefix(sql, "ALTER TABLE "))
		if parseErr == nil {
			relation = f.relations[fakeViewKey(schema, name)]
		}
		if relation == nil {
			// Tables belong to other families or the legacy switch.
			return nil, false, nil
		}
		owner, found := strings.CutPrefix(tail, "OWNER TO ")
		if !found {
			return nil, true, fmt.Errorf("fake view catalog supports only OWNER TO in %q", sql)
		}
		relation.owner = strings.ReplaceAll(strings.Trim(owner, `"`), `""`, `"`)
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER MATERIALIZED VIEW "):
		schema, name, tail, err := fakeViewTarget(strings.TrimPrefix(sql, "ALTER MATERIALIZED VIEW "))
		if err != nil {
			return nil, true, err
		}
		relation := f.relations[fakeViewKey(schema, name)]
		if relation == nil || !relation.materialized {
			return nil, true, errors.New("materialized view does not exist")
		}
		switch {
		case tail == "AUTO REFRESH YES", tail == "AUTO REFRESH NO":
			relation.autoRefresh = tail == "AUTO REFRESH YES"
		case fakeMaterializedViewStoragePattern.MatchString(tail):
			relation.storage = append(relation.storage, tail)
		default:
			return nil, true, fmt.Errorf("fake view catalog does not support %q", sql)
		}
		return nil, true, nil
	case strings.HasPrefix(sql, "DROP VIEW "), strings.HasPrefix(sql, "DROP MATERIALIZED VIEW "):
		materialized := strings.HasPrefix(sql, "DROP MATERIALIZED VIEW ")
		schema, name, _, err := fakeViewTarget(strings.TrimPrefix(strings.TrimPrefix(sql, "DROP MATERIALIZED VIEW "), "DROP VIEW "))
		if err != nil {
			return nil, true, err
		}
		relation := f.relations[fakeViewKey(schema, name)]
		if relation == nil || relation.materialized != materialized {
			return nil, true, fmt.Errorf("view %q does not exist", name)
		}
		delete(f.relations, fakeViewKey(schema, name))
		return nil, true, nil
	}
	return nil, false, nil
}
