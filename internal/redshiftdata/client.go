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
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// API is the AWS statement execution surface required by Client.
type API interface {
	// ExecuteStatement submits one IAM-authenticated SQL statement.
	ExecuteStatement(context.Context, *redshiftdata.ExecuteStatementInput, ...func(*redshiftdata.Options)) (*redshiftdata.ExecuteStatementOutput, error)
	// DescribeStatement reports execution state and result availability.
	DescribeStatement(context.Context, *redshiftdata.DescribeStatementInput, ...func(*redshiftdata.Options)) (*redshiftdata.DescribeStatementOutput, error)
	// GetStatementResult retrieves a page of result rows and column metadata.
	GetStatementResult(context.Context, *redshiftdata.GetStatementResultInput, ...func(*redshiftdata.Options)) (*redshiftdata.GetStatementResultOutput, error)
	// CancelStatement requests cancellation of an outstanding statement.
	CancelStatement(context.Context, *redshiftdata.CancelStatementInput, ...func(*redshiftdata.Options)) (*redshiftdata.CancelStatementOutput, error)
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
		if ctx.Err() != nil {
			// The request may have reached AWS before the context ended, and without an ID it cannot be cancelled.
			err = fmt.Errorf("%w; the statement may have been submitted and still be running", err)
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

// abandon cancels a statement that may still be running, using a detached context because the query context may
// already have ended, and reports a cancellation failure alongside the original error.
func (c *Client) abandon(id *string, cause error) error {
	ctx, cancel := context.WithTimeout(context.Background(), cancelTimeout)
	defer cancel()
	if _, err := c.API.CancelStatement(ctx, &redshiftdata.CancelStatementInput{Id: id}); err != nil {
		return errors.Join(cause, fmt.Errorf("cancel statement %s; it may still be running: %w", *id, err))
	}
	return cause
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
