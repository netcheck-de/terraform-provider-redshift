package provider

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// configurationParameterName matches the names SHOW accepts; SHOW takes parameter and session context names unquoted,
// so each dotted segment must be an identifier.
var configurationParameterName = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,126}(\.[a-z_][a-z0-9_]{0,126})*$`)

// readConfigurationStatement renders SHOW for one configuration parameter.
// https://docs.aws.amazon.com/redshift/latest/dg/r_SHOW.html
func readConfigurationStatement(name string) (string, error) {
	if !configurationParameterName.MatchString(name) {
		return "", fmt.Errorf("parameter name %q must be a lowercase identifier or dotted session context name, such as search_path or app_context.user_id", name)
	}
	if name == "all" {
		// SHOW ALL lists every parameter instead of reading one.
		return "", fmt.Errorf("parameter name must name one parameter, not all")
	}
	//sql:trusted the name matched configurationParameterName, which admits only unquoted identifier segments and dots.
	return sqlclient.Stmt("SHOW").Kw(sqlclient.Keyword(name)).String(), nil
}

// configurationValue extracts the value of a SHOW result. The column is named after the parameter, but its
// spelling can differ, as PostgreSQL's TimeZone does, so a single-column row is read whatever its name.
func configurationValue(name string, rows []sqlclient.Row) (string, error) {
	if len(rows) != 1 {
		return "", fmt.Errorf("SHOW %s returned %d rows, expected 1", name, len(rows))
	}
	row := rows[0]
	for column, value := range row {
		if strings.EqualFold(column, name) {
			return value, nil
		}
	}
	if len(row) == 1 {
		for _, value := range row {
			return value, nil
		}
	}
	return "", fmt.Errorf("SHOW %s returned no column named after the parameter", name)
}
