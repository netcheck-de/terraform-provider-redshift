package provider

import "github.com/hashicorp/terraform-plugin-framework/attr"

// alterStep renders the in-place change of one attribute. Each step emits its own statements, so an update
// changes only what differs and fakes can match one statement per option.
type alterStep[M any] struct {
	// attribute is the schema attribute name the step owns; assertAlterCoverage checks it against the schema.
	attribute string
	// value extracts the attribute from a model to detect a change.
	value func(M) attr.Value
	// render returns the statements that move the object from prev to plan.
	render func(prev, plan M) []string
	// skipNullPrior leaves the attribute alone when the prior value is null, for example state written before
	// the attribute existed, where nothing is known about the object to change from.
	skipNullPrior bool
}

// alterStatements returns the statements of every step whose value differs between prev and plan, in step order.
// Unknown plan values are skipped because they are only resolved by a later read.
func alterStatements[M any](prev, plan M, steps []alterStep[M]) []string {
	var statements []string
	for _, step := range steps {
		before, after := step.value(prev), step.value(plan)
		if after.IsUnknown() || before.Equal(after) || step.skipNullPrior && before.IsNull() {
			continue
		}
		statements = append(statements, step.render(prev, plan)...)
	}
	return statements
}
