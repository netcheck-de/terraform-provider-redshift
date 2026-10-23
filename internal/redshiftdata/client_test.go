package redshiftdata

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata/types"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiStub provides controllable Data API responses and cancellation accounting.
type apiStub struct {
	// execute overrides statement submission when a test needs specific inputs or failures.
	execute func(*redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error)
	// describe overrides execution status polling, including cancellation behavior.
	describe func(context.Context) (*redshiftdata.DescribeStatementOutput, error)
	// result supplies paginated result sets or retrieval failures.
	result func(*redshiftdata.GetStatementResultInput) (*redshiftdata.GetStatementResultOutput, error)
	// cancelError simulates a failed cancellation request.
	cancelError error
	// cancelRefused reports cancellation requests as not accepted.
	cancelRefused bool
	// afterCancel is the status reported once a cancellation was requested; empty selects ABORTED, and Started keeps
	// the statement running.
	afterCancel types.StatusString
	// afterCancelError fails status requests once a cancellation was requested.
	afterCancelError error
	// cancels counts attempted cancellation calls.
	cancels int
	// waits records the WaitTimeSeconds of each status request.
	waits []*int32
}

// ExecuteStatement dispatches to the test callback or returns a successful statement ID.
func (s *apiStub) ExecuteStatement(_ context.Context, input *redshiftdata.ExecuteStatementInput, _ ...func(*redshiftdata.Options)) (*redshiftdata.ExecuteStatementOutput, error) {
	if s.execute != nil {
		return s.execute(input)
	}
	return &redshiftdata.ExecuteStatementOutput{Id: aws.String("statement")}, nil
}

// DescribeStatement dispatches polling to a callback or reports completed execution.
func (s *apiStub) DescribeStatement(ctx context.Context, input *redshiftdata.DescribeStatementInput, _ ...func(*redshiftdata.Options)) (*redshiftdata.DescribeStatementOutput, error) {
	s.waits = append(s.waits, input.WaitTimeSeconds)
	if s.cancels > 0 && s.afterCancelError != nil {
		return nil, s.afterCancelError
	}
	if s.cancels > 0 {
		return &redshiftdata.DescribeStatementOutput{Status: cmp.Or(s.afterCancel, types.StatusStringAborted)}, nil
	}
	if s.describe != nil {
		return s.describe(ctx)
	}
	return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringFinished}, nil
}

// GetStatementResult dispatches result pagination to the configured test callback.
func (s *apiStub) GetStatementResult(_ context.Context, input *redshiftdata.GetStatementResultInput, _ ...func(*redshiftdata.Options)) (*redshiftdata.GetStatementResultOutput, error) {
	return s.result(input)
}

// CancelStatement records cancellation attempts for timeout verification.
func (s *apiStub) CancelStatement(_ context.Context, _ *redshiftdata.CancelStatementInput, _ ...func(*redshiftdata.Options)) (*redshiftdata.CancelStatementOutput, error) {
	s.cancels++
	if s.cancelError != nil {
		return nil, s.cancelError
	}
	return &redshiftdata.CancelStatementOutput{Status: aws.Bool(!s.cancelRefused)}, nil
}

// TestQueryIAMAuthentication checks workgroup routing and deterministic named-parameter binding.
func TestQueryIAMAuthentication(t *testing.T) {
	s := &apiStub{execute: func(input *redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
		assert.Nil(t, input.SecretArn)
		assert.Nil(t, input.DbUser)
		assert.Equal(t, "warehouse", aws.ToString(input.WorkgroupName))
		assert.Equal(t, "admin", aws.ToString(input.Database))
		require.Len(t, input.Parameters, 2)
		assert.Equal(t, "a", *input.Parameters[0].Name)
		assert.Equal(t, "b", *input.Parameters[1].Name)
		return &redshiftdata.ExecuteStatementOutput{Id: aws.String("statement")}, nil
	}}
	client := Client{API: s, Workgroup: "warehouse", Timeout: time.Second, Poll: time.Millisecond}
	rows, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT :a, :b", map[string]string{"b": "two", "a": "one"})
	require.NoError(t, err)
	assert.Empty(t, rows)
}

// TestQueryPollingAndPagination checks polling, pagination, and scalar result normalization.
func TestQueryPollingAndPagination(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// One statement result keeps the same columns on every page.
		columns := []types.ColumnMetadata{{Name: aws.String("text")}, {Name: aws.String("enabled")}, {Name: aws.String("number")}}
		polls := 0
		s := &apiStub{
			describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
				polls++
				if polls == 1 {
					return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
				}
				return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringFinished, HasResultSet: aws.Bool(true)}, nil
			},
			result: func(input *redshiftdata.GetStatementResultInput) (*redshiftdata.GetStatementResultOutput, error) {
				if input.NextToken == nil {
					return &redshiftdata.GetStatementResultOutput{
						ColumnMetadata: columns,
						Records:        [][]types.Field{{&types.FieldMemberStringValue{Value: "value"}, &types.FieldMemberBooleanValue{Value: true}, &types.FieldMemberLongValue{Value: 2}}},
						NextToken:      aws.String("next"),
					}, nil
				}
				assert.Equal(t, "next", *input.NextToken)
				return &redshiftdata.GetStatementResultOutput{
					ColumnMetadata: columns,
					Records:        [][]types.Field{{&types.FieldMemberIsNull{Value: true}, &types.FieldMemberBooleanValue{Value: false}, &types.FieldMemberDoubleValue{Value: 1.5}}},
				}, nil
			},
		}
		client := Client{API: s}
		started := time.Now()
		rows, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT values", nil)
		want := []sqlclient.Row{{"text": "value", "enabled": "true", "number": "2"}, {"text": "", "enabled": "false", "number": "1.5"}}
		require.NoError(t, err)
		assert.Equal(t, want, rows)
		assert.Equal(t, 2, polls)
		assert.Equal(t, DefaultPoll, time.Since(started))
	})
}

// TestQueryFailures checks statement errors, missing IDs, and malformed result metadata.
func TestQueryFailures(t *testing.T) {
	for _, status := range []types.StatusString{types.StatusStringFailed, types.StatusStringAborted} {
		t.Run(string(status), func(t *testing.T) {
			s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
				return &redshiftdata.DescribeStatementOutput{Status: status, Error: aws.String("permission denied")}, nil
			}}
			client := Client{API: s, Timeout: time.Second}
			_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
			require.ErrorContains(t, err, "permission denied")
		})
	}
	for _, operation := range []string{"execute", "describe", "results", "missing ID", "metadata", "unsupported field"} {
		t.Run(operation, func(t *testing.T) {
			s := &apiStub{}
			s.describe = func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
				return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringFinished, HasResultSet: aws.Bool(true)}, nil
			}
			s.result = func(*redshiftdata.GetStatementResultInput) (*redshiftdata.GetStatementResultOutput, error) {
				return nil, errors.New("result error")
			}
			switch operation {
			case "execute":
				s.execute = func(*redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
					return nil, errors.New("execute error")
				}
			case "describe":
				s.describe = func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
					return nil, errors.New("describe error")
				}
			case "missing ID":
				s.execute = func(*redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
					return &redshiftdata.ExecuteStatementOutput{}, nil
				}
			case "metadata", "unsupported field":
				s.result = func(*redshiftdata.GetStatementResultInput) (*redshiftdata.GetStatementResultOutput, error) {
					page := &redshiftdata.GetStatementResultOutput{Records: [][]types.Field{{&types.FieldMemberBlobValue{Value: []byte{1}}}}}
					if operation == "unsupported field" {
						page.ColumnMetadata = []types.ColumnMetadata{{Name: aws.String("blob")}}
					}
					return page, nil
				}
			}
			client := Client{API: s, Timeout: time.Second}
			_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
			require.Error(t, err)
		})
	}
}

// TestQueryCancellation checks bounded statement cancellation during polling and describe calls.
func TestQueryCancellation(t *testing.T) {
	for _, duringDescribe := range []bool{false, true} {
		t.Run(strconv.FormatBool(duringDescribe), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := &apiStub{describe: func(ctx context.Context) (*redshiftdata.DescribeStatementOutput, error) {
					if duringDescribe {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
				}}
				client := Client{API: s, Timeout: time.Minute, Poll: time.Hour}
				started := time.Now()
				_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				assert.Equal(t, 1, s.cancels)
				assert.Equal(t, time.Minute, time.Since(started))
			})
		})
	}
}

// TestQueryWarehouseAuthentication verifies mutually exclusive routing and password-secret/temporary-user requests.
func TestQueryWarehouseAuthentication(t *testing.T) {
	for _, mode := range []string{"cluster user", "cluster secret", "serverless secret"} {
		client := Client{Cluster: "cluster", DBUser: "reader", Timeout: time.Second}
		if mode == "cluster secret" {
			client.DBUser, client.SecretARN = "", "secret"
		}
		if mode == "serverless secret" {
			client.Cluster, client.DBUser, client.Workgroup, client.SecretARN = "", "", "warehouse", "secret"
		}
		client.API = &apiStub{execute: func(input *redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
			if client.Workgroup != "" {
				assert.Nil(t, input.ClusterIdentifier)
				assert.Equal(t, "warehouse", aws.ToString(input.WorkgroupName))
			} else {
				assert.Nil(t, input.WorkgroupName)
				assert.Equal(t, "cluster", aws.ToString(input.ClusterIdentifier))
			}
			if client.SecretARN != "" {
				assert.Equal(t, "secret", aws.ToString(input.SecretArn))
				assert.Nil(t, input.DbUser)
			} else {
				assert.Equal(t, "reader", aws.ToString(input.DbUser))
				assert.Nil(t, input.SecretArn)
			}
			return &redshiftdata.ExecuteStatementOutput{Id: aws.String("statement")}, nil
		}}
		_, err := client.Query(context.Background(), sqlclient.Connection{Database: "analytics"}, "SELECT 1", nil)
		require.NoError(t, err)
	}
}

// TestWaitSecondsLeavesResponseMargin keeps long polls within the Data API limit and the query deadline.
func TestWaitSecondsLeavesResponseMargin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		assert.Equal(t, int32(30), aws.ToInt32(waitSeconds(context.Background())))
		ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
		defer cancel()
		assert.Equal(t, int32(30), aws.ToInt32(waitSeconds(ctx)))
		ctx, cancel = context.WithTimeout(context.Background(), 10500*time.Millisecond)
		defer cancel()
		assert.Equal(t, int32(9), aws.ToInt32(waitSeconds(ctx)))
		ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		assert.Nil(t, waitSeconds(ctx))
	})
}

// TestQuerySubmitsWithoutLongPoll returns the statement ID from ExecuteStatement at once, so an interrupted
// submission never hides a running statement, and long-polls only the status requests.
func TestQuerySubmitsWithoutLongPoll(t *testing.T) {
	for _, name := range []string{"", "terraform-provider-redshift/test"} {
		t.Run(name, func(t *testing.T) {
			s := &apiStub{
				execute: func(input *redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
					assert.Nil(t, input.WaitTimeSeconds)
					if name == "" {
						assert.Nil(t, input.StatementName)
					} else {
						assert.Equal(t, name, aws.ToString(input.StatementName))
					}
					return &redshiftdata.ExecuteStatementOutput{Id: aws.String("statement")}, nil
				},
				describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
					return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringFailed, Error: aws.String("syntax error")}, nil
				},
			}
			client := Client{API: s, Workgroup: "warehouse", StatementName: name, Timeout: time.Minute}
			_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
			require.ErrorContains(t, err, "syntax error")
			require.Len(t, s.waits, 1)
			assert.Equal(t, int32(30), aws.ToInt32(s.waits[0]))
			assert.Zero(t, s.cancels)
		})
	}
}

// TestQueryInterruptedDuringSubmission cancels a statement whose ID arrived as Terraform interrupted the operation,
// and reports that a statement whose submission was cut short may still be running.
func TestQueryInterruptedDuringSubmission(t *testing.T) {
	for _, idReturned := range []bool{false, true} {
		t.Run(strconv.FormatBool(idReturned), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &apiStub{
				execute: func(*redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
					cancel()
					if idReturned {
						return &redshiftdata.ExecuteStatementOutput{Id: aws.String("statement")}, nil
					}
					return nil, context.Canceled
				},
				describe: func(ctx context.Context) (*redshiftdata.DescribeStatementOutput, error) {
					return nil, ctx.Err()
				},
			}
			client := Client{API: s, Timeout: time.Minute}
			_, err := client.Query(ctx, sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
			require.ErrorIs(t, err, context.Canceled)
			if idReturned {
				assert.Equal(t, 1, s.cancels)
				require.NotErrorIs(t, err, sqlclient.ErrUnsettled)
			} else {
				require.ErrorContains(t, err, "may have been submitted: the statement may still be running")
				require.ErrorIs(t, err, sqlclient.ErrUnsettled)
				assert.Zero(t, s.cancels)
			}
		})
	}
}

// TestQueryRetriesRefusedLongPolls treats too many concurrent long polls as a still-running statement, and keeps
// status requests that return early at least one poll interval apart.
func TestQueryRetriesRefusedLongPolls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		polls := 0
		s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
			polls++
			switch polls {
			case 1:
				return nil, &types.ActiveWaitingRequestsExceededException{Message: aws.String("too many waiters")}
			case 2:
				return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
			}
			return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringFinished}, nil
		}}
		client := Client{API: s, Timeout: time.Minute}
		started := time.Now()
		_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
		require.NoError(t, err)
		assert.Equal(t, 3, polls)
		assert.Zero(t, s.cancels)
		assert.Equal(t, 2*DefaultPoll, time.Since(started))
	})
}

// TestQueryCancelsAfterStatusErrors cancels a statement whose status can no longer be observed and reports a failed
// cancellation together with the original error while the statement has not stopped.
func TestQueryCancelsAfterStatusErrors(t *testing.T) {
	describeError, cancelError := errors.New("access denied"), errors.New("cancel refused")
	for _, cancelFails := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelFails), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
					return nil, describeError
				}, afterCancel: types.StatusStringStarted}
				if cancelFails {
					s.cancelError = cancelError
				}
				client := Client{API: s, Timeout: time.Minute}
				_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
				require.ErrorIs(t, err, describeError)
				require.ErrorIs(t, err, sqlclient.ErrUnsettled)
				assert.Equal(t, cancelFails, errors.Is(err, cancelError))
				assert.Equal(t, 1, s.cancels)
			})
		})
	}
}

// TestQuerySettlesAbandonedStatements releases an abandoned statement only once Redshift reports a final state, even
// after an accepted cancellation, and keeps a refused or failed cancellation visible while it may still run.
func TestQuerySettlesAbandonedStatements(t *testing.T) {
	describeError, cancelError := errors.New("access denied"), errors.New("cancel throttled")
	for _, cancel := range []string{"accepted", "refused", "failed"} {
		for _, after := range []types.StatusString{types.StatusStringAborted, types.StatusStringFailed, types.StatusStringFinished, types.StatusStringStarted} {
			t.Run(cancel+"/"+string(after), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
						return nil, describeError
					}, afterCancel: after, cancelRefused: cancel == "refused"}
					if cancel == "failed" {
						s.cancelError = cancelError
					}
					client := Client{API: s, Timeout: time.Minute}
					started := time.Now()
					_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "ALTER TABLE sales", nil)
					require.ErrorIs(t, err, describeError)
					assert.Equal(t, 1, s.cancels)
					if after != types.StatusStringStarted {
						require.NotErrorIs(t, err, sqlclient.ErrUnsettled)
						require.NotErrorIs(t, err, cancelError)
						assert.Equal(t, after == types.StatusStringFinished, strings.Contains(err.Error(), "finished before it could be cancelled"))
						assert.Zero(t, time.Since(started))
						return
					}
					require.ErrorIs(t, err, sqlclient.ErrUnsettled)
					assert.Equal(t, cancel == "failed", errors.Is(err, cancelError))
					assert.Equal(t, cancel == "refused", strings.Contains(err.Error(), "was not accepted"))
					assert.Equal(t, settleTimeout, time.Since(started))
				})
			})
		}
	}
}

// TestQuerySettlementKeepsStatusErrors reports why an abandoned statement's state stayed unknown when status requests
// keep failing after cancellation.
func TestQuerySettlementKeepsStatusErrors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		statusError := errors.New("service unavailable")
		s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
			return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
		}, afterCancelError: statusError}
		client := Client{API: s, Timeout: time.Minute, Poll: time.Hour}
		_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "ALTER TABLE sales", nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, err, sqlclient.ErrUnsettled)
		require.ErrorIs(t, err, statusError)
	})
}

// TestQueryFlagsUncertainSubmission marks a failed ExecuteStatement unsettled only when the request may have reached
// AWS: lost in transit, an unreadable response, or a service fault. A client-fault response or an error raised before
// sending means nothing ran.
func TestQueryFlagsUncertainSubmission(t *testing.T) {
	for name, tc := range map[string]struct {
		err       error
		unsettled bool
	}{
		"send":        {&smithyhttp.RequestSendError{Err: errors.New("connection reset")}, true},
		"response":    {&smithy.DeserializationError{Err: errors.New("truncated")}, true},
		"server":      {&smithy.GenericAPIError{Code: "InternalServerException", Fault: smithy.FaultServer}, true},
		"status 503":  {&smithyhttp.ResponseError{Response: &smithyhttp.Response{Response: &http.Response{StatusCode: 503}}, Err: errors.New("unavailable")}, true},
		"validation":  {&types.ValidationException{Message: aws.String("bad SQL")}, false},
		"credentials": {errors.New("failed to refresh cached credentials"), false},
	} {
		t.Run(name, func(t *testing.T) {
			s := &apiStub{execute: func(*redshiftdata.ExecuteStatementInput) (*redshiftdata.ExecuteStatementOutput, error) {
				return nil, tc.err
			}}
			client := Client{API: s, Timeout: time.Minute}
			_, err := client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "GRANT", nil)
			require.ErrorIs(t, err, tc.err)
			assert.Equal(t, tc.unsettled, errors.Is(err, sqlclient.ErrUnsettled))
		})
	}
}

// TestQueryHonorsEarlierCallerDeadline lets a shorter deadline of the calling operation override Timeout.
func TestQueryHonorsEarlierCallerDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
			return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
		}}
		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		client := Client{API: s, Timeout: time.Hour}
		started := time.Now()
		_, err := client.Query(ctx, sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 1, s.cancels)
		assert.Equal(t, 1500*time.Millisecond, time.Since(started))
		require.Len(t, s.waits, 3, "two polls, then the settlement check after cancellation")
		for _, wait := range s.waits[:2] {
			assert.Nil(t, wait)
		}
	})
}

// TestQueryCancelsOnCallerCancellation cancels the statement when Terraform interrupts the operation.
func TestQueryCancelsOnCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &apiStub{describe: func(context.Context) (*redshiftdata.DescribeStatementOutput, error) {
		cancel()
		return &redshiftdata.DescribeStatementOutput{Status: types.StatusStringStarted}, nil
	}}
	client := Client{API: s, Timeout: time.Minute, Poll: time.Minute}
	_, err := client.Query(ctx, sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 1, s.cancels)
}
