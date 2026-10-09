// Package redshiftdata executes synchronous SQL through the IAM-authenticated Redshift Data API.
package redshiftdata

import (
	"cmp"
	"context"
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
	// Timeout bounds a complete query, including polling and result retrieval; zero selects DefaultTimeout.
	Timeout time.Duration
	// Poll controls the delay between statement status checks; zero selects DefaultPoll.
	Poll time.Duration
}

const (
	// DefaultTimeout bounds a query when Client.Timeout is unset.
	DefaultTimeout = 5 * time.Minute
	// DefaultPoll is the status-check interval when Client.Poll is unset.
	DefaultPoll = time.Second
)

// Verify at compile time that the Data API adapter implements the neutral SQL contract.
var _ sqlclient.Client = (*Client)(nil)

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
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		input.Parameters = append(input.Parameters, types.SqlParameter{Name: aws.String(key), Value: aws.String(parameters[key])})
	}
	statement, err := c.API.ExecuteStatement(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("execute statement in %s/%s: %w", cmp.Or(c.Workgroup, c.Cluster), connection.Database, err)
	}
	if statement.Id == nil || *statement.Id == "" {
		return nil, fmt.Errorf("data API returned no statement ID")
	}
	for {
		status, err := c.API.DescribeStatement(ctx, &redshiftdata.DescribeStatementInput{Id: statement.Id})
		if err != nil {
			if ctx.Err() != nil {
				c.cancel(statement.Id)
			}
			return nil, fmt.Errorf("describe statement %s: %w", *statement.Id, err)
		}
		switch status.Status {
		case types.StatusStringFailed, types.StatusStringAborted:
			return nil, fmt.Errorf("statement %s %s: %s", *statement.Id, status.Status, aws.ToString(status.Error))
		case types.StatusStringFinished:
			if !aws.ToBool(status.HasResultSet) {
				return nil, nil
			}
			return c.results(ctx, statement.Id)
		}
		select {
		case <-ctx.Done():
			c.cancel(statement.Id)
			return nil, fmt.Errorf("wait for statement %s: %w", *statement.Id, ctx.Err())
		case <-time.After(cmp.Or(c.Poll, DefaultPoll)):
		}
	}
}

// cancel attempts bounded cleanup independently of the canceled query context.
func (c *Client) cancel(id *string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Preserve the original timeout/cancellation error if cancellation also fails.
	_, _ = c.API.CancelStatement(ctx, &redshiftdata.CancelStatementInput{Id: id})
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
