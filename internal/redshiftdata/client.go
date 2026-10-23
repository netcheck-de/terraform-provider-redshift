// Package redshiftdata executes synchronous SQL through the IAM-authenticated Redshift Data API.
package redshiftdata

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// API is the AWS statement execution surface required by Client.
type API interface {
	// ExecuteStatement submits one IAM-authenticated SQL statement.
	ExecuteStatement(ctx context.Context, input *redshiftdata.ExecuteStatementInput, options ...func(*redshiftdata.Options)) (*redshiftdata.ExecuteStatementOutput, error)
	// DescribeStatement reports execution state and result availability.
	DescribeStatement(ctx context.Context, input *redshiftdata.DescribeStatementInput, options ...func(*redshiftdata.Options)) (*redshiftdata.DescribeStatementOutput, error)
	// GetStatementResult retrieves a page of result rows and column metadata.
	GetStatementResult(ctx context.Context, input *redshiftdata.GetStatementResultInput, options ...func(*redshiftdata.Options)) (*redshiftdata.GetStatementResultOutput, error)
	// CancelStatement requests cancellation of an outstanding statement.
	CancelStatement(ctx context.Context, input *redshiftdata.CancelStatementInput, options ...func(*redshiftdata.Options)) (*redshiftdata.CancelStatementOutput, error)
}

// Client routes SQL to one workgroup and handles statement polling and result pagination.
type Client struct {
	// API is the authenticated AWS statement client or a test implementation.
	API API
	// Workgroup routes all queries to one Serverless warehouse.
	Workgroup string
	// Cluster routes queries to a provisioned warehouse instead of Workgroup.
	Cluster string
	// DBUser selects temporary cluster credentials when SecretARN is empty.
	DBUser string
	// SecretARN selects Secrets Manager authentication for either warehouse type.
	SecretARN string
	// StatementName labels each statement in Data API listings; empty leaves it unset.
	StatementName string
	// Timeout bounds a complete query, including polling and result retrieval; zero selects DefaultTimeout.
	Timeout time.Duration
	// Poll is the minimum delay between status checks that return without a final state; zero selects DefaultPoll.
	Poll time.Duration
}

const (
	// DefaultTimeout bounds a query when Client.Timeout is unset.
	DefaultTimeout = 5 * time.Minute
	// DefaultPoll is the minimum status-check interval when Client.Poll is unset.
	DefaultPoll = time.Second
	// maxWait is the Data API limit for WaitTimeSeconds.
	maxWait = 30 * time.Second
	// cancelTimeout bounds best-effort cancellation after the query context has ended.
	cancelTimeout = 5 * time.Second
	// settleTimeout bounds the wait for an abandoned statement to reach a final state after cancellation.
	settleTimeout = 30 * time.Second
)

// Verify at compile time that the Data API adapter implements the neutral SQL contract.
var _ sqlclient.Client = (*Client)(nil)

// waitSeconds returns the long-poll duration that still leaves a second of the deadline for the response, or nil when
// too little time remains to wait server-side at all.
func waitSeconds(ctx context.Context) *int32 {
	wait := maxWait
	if deadline, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(deadline)-time.Second)
	}
	if wait < time.Second {
		return nil
	}
	return aws.Int32(int32(wait / time.Second))
}

// Query uses IAM authentication; AWS obtains temporary database credentials.
func (c *Client) Query(ctx context.Context, connection sqlclient.Connection, sql string, parameters map[string]string) ([]sqlclient.Row, error) {
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(c.Timeout, DefaultTimeout))
	defer cancel()
	input := &redshiftdata.ExecuteStatementInput{
		Database: aws.String(connection.Database),
		Sql:      aws.String(sql),
	}
	if c.Workgroup != "" {
		input.WorkgroupName = aws.String(c.Workgroup)
	} else {
		input.ClusterIdentifier = aws.String(c.Cluster)
	}
	if c.SecretARN != "" {
		input.SecretArn = aws.String(c.SecretARN)
	} else if c.DBUser != "" {
		input.DbUser = aws.String(c.DBUser)
	}
	if c.StatementName != "" {
		input.StatementName = aws.String(c.StatementName)
	}
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		input.Parameters = append(input.Parameters, types.SqlParameter{Name: aws.String(key), Value: aws.String(parameters[key])})
	}
	// Submission deliberately does not long-poll: the statement runs while a long-polled ExecuteStatement is pending, and
	// an interrupted call returns no ID to cancel it with. Without WaitTimeSeconds the ID comes back at once.
	statement, err := c.API.ExecuteStatement(ctx, input)
	if err != nil {
		err = fmt.Errorf("execute statement in %s/%s: %w", cmp.Or(c.Workgroup, c.Cluster), connection.Database, err)
		if submissionUncertain(ctx, err) {
			// The request may have reached AWS, and without an ID the statement cannot be cancelled or observed.
			err = fmt.Errorf("%w; it may have been submitted: %w", err, sqlclient.ErrUnsettled)
		}
		return nil, err
	}
	if statement.Id == nil || *statement.Id == "" {
		return nil, fmt.Errorf("data API returned no statement ID")
	}
	return c.wait(ctx, statement.Id)
}

// wait long-polls the statement status until it reaches a final state, cancelling it when the wait is abandoned.
func (c *Client) wait(ctx context.Context, id *string) ([]sqlclient.Row, error) {
	for {
		started := time.Now()
		status, err := c.API.DescribeStatement(ctx, &redshiftdata.DescribeStatementInput{Id: id, WaitTimeSeconds: waitSeconds(ctx)})
		var waiting *types.ActiveWaitingRequestsExceededException
		switch {
		case errors.As(err, &waiting) && ctx.Err() == nil:
			// Too many concurrent long polls on this statement; it is still running, so check again after the poll interval.
		case err != nil:
			return nil, c.abandon(id, fmt.Errorf("describe statement %s: %w", *id, err))
		case status.Status == types.StatusStringFailed, status.Status == types.StatusStringAborted:
			return nil, fmt.Errorf("statement %s %s: %s", *id, status.Status, aws.ToString(status.Error))
		case status.Status == types.StatusStringFinished:
			if !aws.ToBool(status.HasResultSet) {
				return nil, nil
			}
			return c.results(ctx, id)
		}
		// A refused or ignored long poll returns at once; the poll interval keeps that from becoming a busy loop.
		select {
		case <-ctx.Done():
			return nil, c.abandon(id, fmt.Errorf("wait for statement %s: %w", *id, ctx.Err()))
		case <-time.After(cmp.Or(c.Poll, DefaultPoll) - time.Since(started)):
		}
	}
}

// submissionUncertain reports whether a failed ExecuteStatement may still have started the statement: the context
// ended, the request or its response was lost in transit, or the service failed while handling it. Errors raised before
// sending, such as credential failures, and client-fault responses mean nothing ran.
func submissionUncertain(ctx context.Context, err error) bool {
	var send *smithyhttp.RequestSendError
	var decode *smithy.DeserializationError
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	return ctx.Err() != nil || errors.As(err, &send) || errors.As(err, &decode) ||
		(errors.As(err, &response) && response.HTTPStatusCode() >= 500) ||
		(errors.As(err, &api) && api.ErrorFault() == smithy.FaultServer)
}

// abandon cancels a statement that may still be running and waits until Redshift reports a final state, using a
// detached context because the query context may already have ended. The caller still holds the mutation gate, so a
// statement that cannot be shown to have stopped wraps sqlclient.ErrUnsettled together with any cancellation failure.
func (c *Client) abandon(id *string, cause error) error {
	var problems []error
	cancelCtx, cancelDone := context.WithTimeout(context.Background(), cancelTimeout)
	cancelled, err := c.API.CancelStatement(cancelCtx, &redshiftdata.CancelStatementInput{Id: id})
	cancelDone()
	switch {
	case err != nil:
		problems = append(problems, fmt.Errorf("cancel statement %s: %w", *id, err))
	case !aws.ToBool(cancelled.Status):
		problems = append(problems, fmt.Errorf("cancel statement %s was not accepted", *id))
	}
	ctx, cancel := context.WithTimeout(context.Background(), settleTimeout)
	defer cancel()
	var observed error
	for {
		started := time.Now()
		status, err := c.API.DescribeStatement(ctx, &redshiftdata.DescribeStatementInput{Id: id, WaitTimeSeconds: waitSeconds(ctx)})
		var waiting *types.ActiveWaitingRequestsExceededException
		switch {
		case err == nil && (status.Status == types.StatusStringFailed || status.Status == types.StatusStringAborted):
			return cause
		case err == nil && status.Status == types.StatusStringFinished:
			return fmt.Errorf("%w; statement %s finished before it could be cancelled", cause, *id)
		case err != nil && !errors.As(err, &waiting) && ctx.Err() == nil:
			observed = fmt.Errorf("describe statement %s after cancellation: %w", *id, err)
		}
		select {
		case <-ctx.Done():
			problems = append(problems, observed, fmt.Errorf("statement %s did not reach a final state within %s after cancellation: %w", *id, settleTimeout, sqlclient.ErrUnsettled))
			return errors.Join(append([]error{cause}, problems...)...)
		case <-time.After(cmp.Or(c.Poll, DefaultPoll) - time.Since(started)):
		}
	}
}

// results reads all result pages and normalizes supported scalar fields to textual rows.
func (c *Client) results(ctx context.Context, id *string) ([]sqlclient.Row, error) {
	input := &redshiftdata.GetStatementResultInput{Id: id}
	var rows []sqlclient.Row
	for {
		page, err := c.API.GetStatementResult(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("get results for statement %s: %w", *id, err)
		}
		for _, record := range page.Records {
			if len(record) != len(page.ColumnMetadata) {
				return nil, fmt.Errorf("statement %s returned inconsistent column metadata", *id)
			}
			row := make(sqlclient.Row, len(record))
			for i, field := range record {
				name := aws.ToString(page.ColumnMetadata[i].Name)
				switch field := field.(type) {
				case *types.FieldMemberStringValue:
					row[name] = field.Value
				case *types.FieldMemberBooleanValue:
					row[name] = strconv.FormatBool(field.Value)
				case *types.FieldMemberLongValue:
					row[name] = strconv.FormatInt(field.Value, 10)
				case *types.FieldMemberDoubleValue:
					row[name] = strconv.FormatFloat(field.Value, 'g', -1, 64)
				case *types.FieldMemberIsNull:
					row[name] = ""
				default:
					return nil, fmt.Errorf("statement %s returned unsupported field %T", *id, field)
				}
			}
			rows = append(rows, row)
		}
		if aws.ToString(page.NextToken) == "" {
			return rows, nil
		}
		input.NextToken = page.NextToken
	}
}
