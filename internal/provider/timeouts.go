package provider

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

// timeoutsBlockName names the block that bounds a resource's create, update, and delete. It configures Terraform
// operations rather than the SQL object, so lookups, replacement policies, and alter coverage skip it.
const timeoutsBlockName = "timeouts"

// operationTimeoutsBlock declares create, update, and delete timeouts for resources whose operations can outlast a
// single statement. createNote extends the create description where Create also waits for something other than SQL.
func operationTimeoutsBlock(ctx context.Context, createNote string) schema.Block {
	block := timeouts.Block(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}).(schema.SingleNestedBlock)
	block.MarkdownDescription = "Time limits for whole Terraform operations, as durations such as `30m` or `2h`. Unset, an " +
		"operation has no limit of its own and each SQL statement is bounded only by the provider's `query_timeout`; set, " +
		"a statement still running when the operation's limit expires is cancelled. A change to the timeouts alone runs " +
		"no DDL of its own, but applies as an in-place update that reads and verifies the catalog like any other."
	descriptions := map[string]string{
		"create": "Maximum time for creating the object, including follow-up statements and the catalog verification." + createNote,
		"update": "Maximum time for an in-place update, including the catalog reads before and after the change.",
		"delete": "Maximum time for dropping the object and verifying that it is gone. Applies only when the value was " +
			"saved to state by an earlier apply before the destroy.",
	}
	for name, attribute := range block.Attributes {
		value := attribute.(schema.StringAttribute)
		value.Description, value.MarkdownDescription = "", descriptions[name]
		// The library's own validator accepts zero and negative durations, which would end every operation at once.
		value.Validators = []validator.String{durationValidator{}}
		block.Attributes[name] = value
	}
	return block
}

// boundOperation limits ctx by the configured timeout that configured reads, such as timeouts.Value.Create. Without one,
// ctx is returned unchanged. The transports derive each statement's context from ctx, so a statement ends with the
// operation even when its query_timeout lies later. The returned function releases the deadline and, when it expired,
// adds an error that names the timeout, because the transport errors only report an exceeded context deadline.
func boundOperation(ctx context.Context, operation string, configured func(context.Context, time.Duration) (time.Duration, diag.Diagnostics), diagnostics *diag.Diagnostics) (context.Context, func()) {
	timeout, problems := configured(ctx, 0)
	diagnostics.Append(problems...)
	if timeout <= 0 {
		return ctx, func() {}
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	return bounded, func() {
		if ctx.Err() == nil && errors.Is(bounded.Err(), context.DeadlineExceeded) && diagnostics.HasError() {
			diagnostics.AddError("Operation timed out", fmt.Sprintf(
				"The %s timeout of %s expired before the %s finished, and statements still running were cancelled. "+
					"Increase timeouts.%s and apply again.", operation, timeout, operation, operation))
		}
		cancel()
	}
}
