package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/smithy-go"
	"github.com/hashicorp/terraform-plugin-framework/types"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// datashareAPIFunc adapts a callback into the SDK's read-only datashare discovery interface.
type datashareAPIFunc func(context.Context, *redshift.DescribeDataSharesInput) (*redshift.DescribeDataSharesOutput, error)

// DescribeDataShares returns synthetic pages without calling AWS.
func (f datashareAPIFunc) DescribeDataShares(ctx context.Context, input *redshift.DescribeDataSharesInput, _ ...func(*redshift.Options)) (*redshift.DescribeDataSharesOutput, error) {
	return f(ctx, input)
}

// TestDatashareDiscoveryMatchesProducerAcrossPages verifies ARN regions are observed rather than inferred.
func TestDatashareDiscoveryMatchesProducerAcrossPages(t *testing.T) {
	source, err := parseShare(shareARN)
	require.NoError(t, err)
	for _, value := range []string{
		shareARN,
		strings.Replace(shareARN, "eu-central-1", "us-west-2", 1),
		strings.Replace(shareARN, "arn:aws:redshift:eu-central-1", "arn:aws-cn:redshift:cn-north-1", 1),
	} {
		t.Run(value, func(t *testing.T) {
			calls := 0
			api := datashareAPIFunc(func(_ context.Context, input *redshift.DescribeDataSharesInput) (*redshift.DescribeDataSharesOutput, error) {
				calls++
				assert.Nil(t, input.DataShareArn)
				if calls == 1 {
					assert.Nil(t, input.Marker)
					return &redshift.DescribeDataSharesOutput{
						Marker: aws.String("next"),
						DataShares: []redshifttypes.DataShare{
							{}, {DataShareArn: aws.String("invalid")},
							{DataShareArn: aws.String(strings.Replace(shareARN, "/source", "/another", 1))},
							{DataShareArn: aws.String(strings.Replace(shareARN, "123456789012", "222222222222", 1))},
							{DataShareArn: aws.String(strings.Replace(shareARN, "11111111-2222-3333-4444-555555555555", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", 1))},
						},
					}, nil
				}
				assert.Equal(t, "next", aws.ToString(input.Marker))
				return &redshift.DescribeDataSharesOutput{DataShares: []redshifttypes.DataShare{{DataShareArn: aws.String(value)}, {DataShareArn: aws.String(value)}}}, nil
			})
			observed, err := findDatashareARN(context.Background(), api, source)
			require.NoError(t, err)
			assert.Equal(t, value, observed)
			assert.Equal(t, 2, calls)
		})
	}
}

// TestDatashareDiscoveryRejectsMissingAmbiguousAndFailedLookups checks discovery failure diagnostics.
func TestDatashareDiscoveryRejectsMissingAmbiguousAndFailedLookups(t *testing.T) {
	source, err := parseShare(shareARN)
	require.NoError(t, err)
	for _, name := range []string{"missing", "ambiguous", "unavailable"} {
		t.Run(name, func(t *testing.T) {
			api := datashareAPIFunc(func(context.Context, *redshift.DescribeDataSharesInput) (*redshift.DescribeDataSharesOutput, error) {
				if name == "unavailable" {
					return nil, errors.New("access denied")
				}
				page := &redshift.DescribeDataSharesOutput{}
				if name == "ambiguous" {
					page.DataShares = []redshifttypes.DataShare{{DataShareArn: aws.String(shareARN)}, {DataShareArn: aws.String(strings.Replace(shareARN, "eu-central-1", "us-west-2", 1))}}
				}
				return page, nil
			})
			_, err := findDatashareARN(context.Background(), api, source)
			require.Error(t, err)
		})
	}
	for _, value := range []string{
		"arn::redshift:eu-central-1:123456789012:datashare:namespace/source",
		"arn:aws:redshift::123456789012:datashare:namespace/source",
	} {
		_, err := parseShare(value)
		require.Error(t, err)
	}
}

// TestDatashareDiscoveryUsesProviderAWSConfiguration exercises the real SDK against a local fake endpoint.
func TestDatashareDiscoveryUsesProviderAWSConfiguration(t *testing.T) {
	source, err := parseShare(shareARN)
	require.NoError(t, err)
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_REGION", "us-east-1")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "TESTKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "TESTSECRET")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := r.ParseForm()
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		assert.Equal(t, "DescribeDataShares", r.Form.Get("Action"))
		assert.Contains(t, r.Header.Get("Authorization"), "/eu-central-1/redshift/")
		w.Header().Set("Content-Type", "text/xml")
		_, _ = fmt.Fprintf(w, `<DescribeDataSharesResponse xmlns="http://redshift.amazonaws.com/doc/2012-12-01/"><DescribeDataSharesResult><DataShares><member><DataShareArn>%s</DataShareArn></member></DataShares></DescribeDataSharesResult></DescribeDataSharesResponse>`, shareARN)
	}))
	defer server.Close()
	t.Setenv("AWS_ENDPOINT_URL_REDSHIFT", server.URL)
	credentials := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(credentials, []byte("[discovery]\naws_access_key_id = PROFILEKEY\naws_secret_access_key = PROFILESECRET\n"), 0o600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentials)
	for _, profile := range []types.String{types.StringNull(), types.StringValue("discovery")} {
		data := providerModel{Region: types.StringValue("eu-central-1"), Profile: profile}
		observed, err := data.datashareARN(context.Background(), source)
		require.NoError(t, err)
		assert.Equal(t, shareARN, observed)
	}
}

type dataAPIHTTPClientFunc func(*http.Request) (*http.Response, error)

func (f dataAPIHTTPClientFunc) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

// TestDataAPIRetriesUseProviderSettings keeps SDK backoff and adaptive rate limiting on virtual time while
// exercising the provider's real retryer, error classification, and statement application name.
func TestDataAPIRetriesUseProviderSettings(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_REGION", "eu-central-1")
	t.Setenv("AWS_ACCESS_KEY_ID", "TESTKEY")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "TESTSECRET")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_MAX_ATTEMPTS", "")
	t.Setenv("AWS_RETRY_MODE", "")
	for _, retries := range []int64{0, 1} {
		for _, mode := range []string{"standard", "adaptive"} {
			for _, code := range []string{"ActiveStatementsExceededException", "ValidationException"} {
				t.Run(fmt.Sprintf("%s/%d/%s", mode, retries, code), func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						data := providerModel{Workgroup: types.StringValue("warehouse"), Database: types.StringValue("admin"), MaxRetries: types.Int64Value(retries), RetryMode: types.StringValue(mode)}
						client, err := data.sqlClient(context.Background(), "test")
						require.NoError(t, err)
						adapter := client.(*dataapi.Client)
						options := adapter.API.(*redshiftdata.Client).Options()
						attempts := 0
						// Copy every provider-configured option, including the retryer; replace only HTTP I/O.
						adapter.API = redshiftdata.New(options, func(options *redshiftdata.Options) {
							options.HTTPClient = dataAPIHTTPClientFunc(func(r *http.Request) (*http.Response, error) {
								attempts++
								var input struct {
									StatementName   string
									WaitTimeSeconds int32
								}
								assert.Equal(t, "RedshiftData.ExecuteStatement", r.Header.Get("X-Amz-Target"))
								require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
								assert.Equal(t, "terraform-provider-redshift/test", input.StatementName)
								assert.Zero(t, input.WaitTimeSeconds)
								return &http.Response{
									StatusCode: http.StatusBadRequest,
									Header: http.Header{
										"Content-Type":     {"application/x-amz-json-1.1"},
										"X-Amzn-Errortype": {code},
									},
									Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"__type":%q,"Message":"rejected"}`, code))),
								}, nil
							})
						})
						_, err = client.Query(context.Background(), sqlclient.Connection{Database: "admin"}, "SELECT 1", nil)
						require.ErrorContains(t, err, code)
						wantAttempts := int(retries + 1)
						if code == "ValidationException" {
							wantAttempts = 1
						}
						assert.Equal(t, wantAttempts, attempts)
						// A client shared by every resource must not exhaust retries under sustained errors.
						for range 1000 {
							_, err := options.Retryer.GetRetryToken(context.Background(), &smithy.GenericAPIError{Code: code})
							require.NoError(t, err)
						}
					})
				})
			}
		}
	}
}

// TestDatashareDiscoveryRejectsMissingRegionAndBrokenProfiles preserves credential-free local SQL configuration.
func TestDatashareDiscoveryRejectsMissingRegionAndBrokenProfiles(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "absent"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(t.TempDir(), "absent"))
	for _, data := range []providerModel{{}, {Region: types.StringValue("eu-central-1"), Profile: types.StringValue("missing")}} {
		_, err := data.datashareARN(context.Background(), shareSource{})
		require.Error(t, err)
	}
}
