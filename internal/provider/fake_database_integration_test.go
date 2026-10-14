package provider

import (
	"fmt"
	"regexp"
	"strings"

	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// The name sorts before "databases", whose family answers every SHOW DATABASES, so this family sees the
// destination database first.
var _ = registerFakeFamily("database_integration", func() fakeFamily { return newDatabaseIntegrationFamily() })

// fakeIntegrationDatabase is the zero-ETL destination database the family emulates.
const fakeIntegrationDatabase = "replica"

// fakeIntegrationID is the integration the representative destination receives.
const fakeIntegrationID = "a1b2c3d4-5678-90ab-cdef-example11111"

// databaseIntegrationFamily emulates one zero-ETL destination database, its SHOW DATABASES row, its local options,
// and its SVV_INTEGRATION row.
type databaseIntegrationFamily struct {
	// exists records whether the destination database exists.
	exists bool
	// databaseType is the SHOW DATABASES type, which Redshift does not document for a destination.
	databaseType string
	// hidden emulates a provider identity that is not a superuser, which SVV_INTEGRATION shows no rows.
	hidden bool
	// integrationID, sourceDatabase, refreshInterval, historyMode, and autoRemediation are the SVV_INTEGRATION values.
	integrationID, sourceDatabase, refreshInterval, historyMode, autoRemediation string
	// settings records the options no catalog view reports, keyed by keyword, for assertions.
	settings map[string]string
	// owner, connectionLimit, and isolation are the local options.
	owner, connectionLimit, isolation string
}

// newDatabaseIntegrationFamily returns a family whose destination does not exist yet.
func newDatabaseIntegrationFamily() *databaseIntegrationFamily {
	f := &databaseIntegrationFamily{databaseType: "integration"}
	f.reset(fakeIntegrationID, "")
	return f
}

// reset applies the defaults of CREATE DATABASE ... FROM INTEGRATION for a source without a source database.
func (f *databaseIntegrationFamily) reset(integrationID, sourceDatabase string) {
	f.integrationID, f.sourceDatabase = integrationID, sourceDatabase
	f.refreshInterval, f.historyMode, f.autoRemediation = "0", "f", "f"
	f.settings = map[string]string{}
	f.owner, f.connectionLimit, f.isolation = "admin", "UNLIMITED", "Snapshot Isolation"
}

// populate makes the representative destination exist, as fullCatalog does for the legacy objects.
func (f *databaseIntegrationFamily) populate() {
	f.exists = true
}

var (
	// fakeIntegrationSource captures the integration identifier.
	fakeIntegrationSource = regexp.MustCompile(`FROM INTEGRATION '((?:[^']|'')*)'`)
	// fakeIntegrationSetting captures one option of SET.
	fakeIntegrationSetting = regexp.MustCompile(`(ACCEPTINVCHARS|QUERY_ALL_STATES|TRUNCATECOLUMNS|HISTORY_MODE|AUTO_REMEDIATION) = (TRUE|FALSE)|REFRESH_INTERVAL (\d+)`)
)

// apply records the integration options and local options of CREATE or ALTER DATABASE.
func (f *databaseIntegrationFamily) apply(sql string) {
	for _, match := range fakeIntegrationSetting.FindAllStringSubmatch(sql, -1) {
		switch match[1] {
		case "":
			f.refreshInterval = match[3]
		case "HISTORY_MODE":
			f.historyMode = strings.ToLower(match[2][:1])
		case "AUTO_REMEDIATION":
			f.autoRemediation = strings.ToLower(match[2][:1])
		default:
			f.settings[match[1]] = match[2]
		}
	}
	if owner, ok := fakeIdentifier(fakeOwner, sql); ok {
		f.owner = owner
	}
	if match := fakeLimit.FindStringSubmatch(sql); match != nil {
		f.connectionLimit = match[1]
	}
	if match := fakeIsolation.FindStringSubmatch(sql); match != nil {
		f.isolation = map[string]string{"SNAPSHOT": "Snapshot Isolation", "SERIALIZABLE": "Serializable"}[match[1]]
	}
}

// query answers the destination's catalog reads and applies its CREATE, ALTER, and DROP DATABASE statements.
func (f *databaseIntegrationFamily) query(_ *catalog, _ dataapi.Connection, sql string, parameters map[string]string) ([]dataapi.Row, bool, error) {
	quoted := `"` + fakeIntegrationDatabase + `"`
	switch {
	case strings.HasPrefix(sql, "SHOW DATABASES LIKE '"+fakeIntegrationDatabase+"'"):
		if !f.exists {
			return nil, true, nil
		}
		return []dataapi.Row{{"database_name": fakeIntegrationDatabase, "database_owner": "100", "database_type": f.databaseType, "database_isolation_level": f.isolation}}, true, nil
	case strings.HasPrefix(sql, "SELECT u.usename AS owner, d.datconnlimit") && parameters["name"] == fakeIntegrationDatabase:
		if !f.exists {
			return nil, true, nil
		}
		return []dataapi.Row{{"owner": f.owner, "connection_limit": f.connectionLimit}}, true, nil
	case strings.HasPrefix(sql, "SELECT TRIM(integration_id) AS integration_id"):
		if !f.exists || f.hidden || parameters["name"] != fakeIntegrationDatabase {
			return nil, true, nil
		}
		// The view pads its CHAR(128) columns; the query trims them, and the decoder trims again in case it did not.
		return []dataapi.Row{{
			"integration_id": f.integrationID + "  ", "source_database": f.sourceDatabase + "  ", "refresh_interval": f.refreshInterval,
			"is_history_mode": f.historyMode, "auto_remediation": f.autoRemediation,
		}}, true, nil
	case strings.HasPrefix(sql, "CREATE DATABASE "+quoted+" FROM INTEGRATION "):
		if f.exists {
			return nil, true, fmt.Errorf("database already exists")
		}
		source := fakeIntegrationSource.FindStringSubmatch(sql)
		if source == nil {
			return nil, true, fmt.Errorf("fake catalog cannot parse the integration of %q", sql)
		}
		sourceDatabase, _ := fakeLiteral("DATABASE", sql)
		f.reset(commentUnescaper.Replace(source[1]), sourceDatabase)
		f.apply(sql)
		f.exists = true
		return nil, true, nil
	case strings.HasPrefix(sql, "ALTER DATABASE "+quoted+" "):
		if !f.exists {
			return nil, true, fmt.Errorf("database does not exist")
		}
		f.apply(strings.TrimPrefix(sql, "ALTER DATABASE "+quoted))
		return nil, true, nil
	case sql == "DROP DATABASE "+quoted:
		if !f.exists {
			return nil, true, fmt.Errorf("database does not exist")
		}
		f.exists = false
		return nil, true, nil
	}
	return nil, false, nil
}
