package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/hashicorp/terraform-plugin-framework/types"
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
