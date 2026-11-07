package provider

import (
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// readCopyJobDefinition consumes the entire server statement. Unknown syntax is an error, never lost state.
func readCopyJobDefinition(data *copyJobModel, text string) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("SYS_COPY_JOB.copy_query is unavailable or empty; this engine cannot safely refresh the COPY job definition")
	}

	tokens, err := sqlclient.Tokens(text)
	if err != nil {
		return fmt.Errorf("SYS_COPY_JOB.copy_query: %w", err)
	}

	i := 0
	word := func(index int, expected string) bool {
		return index < len(tokens) && tokens[index].Kind == sqlclient.TokenWord && strings.EqualFold(tokens[index].Text, expected)
	}
	symbol := func(index int, expected string) bool {
		return index < len(tokens) && tokens[index].Kind == sqlclient.TokenSymbol && tokens[index].Text == expected
	}
	identifier := func() (string, error) {
		if i >= len(tokens) || tokens[i].Kind != sqlclient.TokenIdentifier && tokens[i].Kind != sqlclient.TokenWord {
			return "", fmt.Errorf("expected an identifier in SYS_COPY_JOB.copy_query")
		}

		value := tokens[i].Text
		if tokens[i].Kind == sqlclient.TokenWord {
			value = strings.ToLower(value)
		}

		i++

		return value, nil
	}

	if !word(i, "COPY") {
		return fmt.Errorf("SYS_COPY_JOB.copy_query does not start with COPY")
	}

	i++

	var names []string

	for {
		name, err := identifier()
		if err != nil {
			return err
		}

		names = append(names, name)

		if !symbol(i, ".") {
			break
		}

		i++
	}

	if len(names) == 3 {
		if names[0] != data.Database.ValueString() {
			return fmt.Errorf("COPY job targets database %q, expected %q", names[0], data.Database.ValueString())
		}

		names = names[1:]
	}

	if len(names) != 2 {
		return fmt.Errorf("SYS_COPY_JOB.copy_query must identify the target schema and table explicitly")
	}

	columns := []attr.Value{}

	if symbol(i, "(") {
		i++

		for {
			column, err := identifier()
			if err != nil {
				return err
			}

			columns = append(columns, types.StringValue(column))

			if !symbol(i, ",") {
				break
			}

			i++
		}

		if !symbol(i, ")") {
			return fmt.Errorf("unterminated COPY job column list")
		}

		i++
	}

	if !word(i, "FROM") || i+1 >= len(tokens) || tokens[i+1].Kind != sqlclient.TokenString {
		return fmt.Errorf("COPY job definition needs FROM with a source literal")
	}

	source := copyLiteralValue(tokens[i+1])

	i += 2
	if !word(i, "IAM_ROLE") || i+1 >= len(tokens) || tokens[i+1].Kind != sqlclient.TokenString {
		return fmt.Errorf("COPY job definition needs IAM_ROLE with a role literal (possibly redacted)")
	}
	// SYS_COPY_JOB redacts this literal; the separate iam_role column is authoritative.
	i += 2

	end := i
	for end < len(tokens) && !word(end, "JOB") {
		end++
	}

	if !word(end, "JOB") || !word(end+1, "CREATE") || end+2 >= len(tokens) || !word(end+2, data.Name.ValueString()) {
		return fmt.Errorf("COPY job definition needs JOB CREATE with the selected job name")
	}

	definition, err := parseCopyOptions(tokens[i:end])
	if err != nil {
		return fmt.Errorf("COPY job definition: %w", err)
	}

	i = end + 3
	if word(i, "AUTO") && (word(i+1, "ON") || word(i+1, "OFF")) {
		i += 2
	}

	if symbol(i, ";") {
		i++
	}

	if i != len(tokens) {
		return fmt.Errorf("unsupported trailing syntax in COPY job definition")
	}

	columnValue := types.ListNull(types.StringType)
	if len(columns) > 0 {
		columnValue = types.ListValueMust(types.StringType, columns)
	}

	data.Table = types.ObjectValueMust(copyJobTableType.AttrTypes, map[string]attr.Value{
		"schema": types.StringValue(names[0]), "name": types.StringValue(names[1]), "columns": columnValue,
	})
	data.Source = types.StringValue(source)
	entry := templateCatalog{definition: definition}
	model := templateModel{Format: data.Format, FormatArgument: data.FormatArgument, Parameters: data.Parameters}
	entry.applyCatalog(&model)
	data.Format, data.FormatArgument = model.Format, model.FormatArgument
	data.Parameters = copyParametersValue(data.Parameters, definition.parameters, true)

	return nil
}
