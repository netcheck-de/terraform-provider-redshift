package redshiftconn

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	clusterTypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	serverlessTypes "github.com/aws/aws-sdk-go-v2/service/redshiftserverless/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// iamStub controls discovery, pagination, and temporary credential responses for both AWS warehouse types.
type iamStub struct {
	// fail selects an API operation to fail.
	fail string
	// workgroup is the returned Serverless endpoint metadata.
	workgroup *serverlessTypes.Workgroup
	// clusters is the returned provisioned endpoint metadata.
	clusters []clusterTypes.Cluster
	// username is the temporary database identity returned by AWS.
	username string
	// password is the temporary SQL secret returned by AWS.
	password string
	// expiration controls cache lifetime and invalid credential tests.
	expiration *time.Time
	// calls counts temporary credential requests per database.
	calls map[string]int
	// absent makes workgroup ARN resolution exhaust its pages without finding the UUID.
	absent bool
}

// GetWorkgroup supplies endpoint metadata or a controlled discovery failure.
func (s *iamStub) GetWorkgroup(context.Context, *redshiftserverless.GetWorkgroupInput, ...func(*redshiftserverless.Options)) (*redshiftserverless.GetWorkgroupOutput, error) {
	if s.fail == "workgroup" {
		return nil, errors.New("workgroup failed")
	}
	return &redshiftserverless.GetWorkgroupOutput{Workgroup: s.workgroup}, nil
}

// ListWorkgroups exercises multi-page ARN-to-name resolution.
func (s *iamStub) ListWorkgroups(_ context.Context, input *redshiftserverless.ListWorkgroupsInput, _ ...func(*redshiftserverless.Options)) (*redshiftserverless.ListWorkgroupsOutput, error) {
	if s.fail == "list" {
		return nil, errors.New("list failed")
	}
	if s.absent {
		return &redshiftserverless.ListWorkgroupsOutput{}, nil
	}
	if input.NextToken == nil {
		return &redshiftserverless.ListWorkgroupsOutput{NextToken: aws.String("next")}, nil
	}
	return &redshiftserverless.ListWorkgroupsOutput{Workgroups: []serverlessTypes.Workgroup{{WorkgroupId: aws.String("uuid"), WorkgroupName: aws.String("warehouse")}}}, nil
}

// GetCredentials returns per-database temporary Serverless authentication.
func (s *iamStub) GetCredentials(_ context.Context, input *redshiftserverless.GetCredentialsInput, _ ...func(*redshiftserverless.Options)) (*redshiftserverless.GetCredentialsOutput, error) {
	if s.fail == "serverless credentials" {
		return nil, errors.New("credentials failed")
	}
	s.calls[aws.ToString(input.DbName)]++
	return &redshiftserverless.GetCredentialsOutput{DbUser: aws.String(s.username), DbPassword: aws.String(s.password), Expiration: s.expiration}, nil
}

// DescribeClusters supplies provisioned routing or a controlled discovery failure.
func (s *iamStub) DescribeClusters(context.Context, *redshift.DescribeClustersInput, ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error) {
	if s.fail == "cluster" {
		return nil, errors.New("cluster failed")
	}
	return &redshift.DescribeClustersOutput{Clusters: s.clusters}, nil
}

// GetClusterCredentials records existing-user requests without creating users or assigning groups.
func (s *iamStub) GetClusterCredentials(_ context.Context, input *redshift.GetClusterCredentialsInput, _ ...func(*redshift.Options)) (*redshift.GetClusterCredentialsOutput, error) {
	if s.fail == "cluster credentials" {
		return nil, errors.New("credentials failed")
	}
	if aws.ToBool(input.AutoCreate) || len(input.DbGroups) != 0 {
		return nil, errors.New("unexpected user/group mutation")
	}
	s.calls[aws.ToString(input.DbName)]++
	return &redshift.GetClusterCredentialsOutput{DbUser: aws.String(s.username), DbPassword: aws.String(s.password), Expiration: s.expiration}, nil
}

// iamTestClient builds a deterministic resolver with valid default metadata and credentials.
func iamTestClient() (*IAM, *iamStub) {
	expiration := time.Now().Add(15 * time.Minute)
	stub := &iamStub{workgroup: &serverlessTypes.Workgroup{Endpoint: &serverlessTypes.Endpoint{Address: aws.String("warehouse.example.com"), Port: aws.Int32(5439)}}, clusters: []clusterTypes.Cluster{{Endpoint: &clusterTypes.Endpoint{Address: aws.String("cluster.example.com"), Port: aws.Int32(5439)}}}, username: "IAM:reader", password: "temporary", expiration: &expiration, calls: map[string]int{}}
	return &IAM{Workgroup: "warehouse", Serverless: stub, Provisioned: stub}, stub
}

// TestIAMCredentialsRefreshByDatabase verifies discovery, caching, refresh margin, and database isolation.
func TestIAMCredentialsRefreshByDatabase(t *testing.T) {
	for _, provisioned := range []bool{false, true} {
		iam, stub := iamTestClient()
		if provisioned {
			iam.Workgroup, iam.Cluster, iam.DBUser = "", "cluster", "reader"
		}
		first, err := iam.Resolve(context.Background(), "admin")
		require.NoError(t, err)
		assert.Equal(t, "IAM:reader", first.Username)
		assert.Equal(t, uint16(5439), first.Port)
		_, err = iam.Resolve(context.Background(), "admin")
		require.NoError(t, err)
		_, err = iam.Resolve(context.Background(), "analytics")
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"admin": 1, "analytics": 1}, stub.calls)
		iam.now = func() time.Time { return stub.expiration.Add(-30 * time.Second) }
		_, err = iam.Resolve(context.Background(), "admin")
		require.NoError(t, err)
		assert.Equal(t, 2, stub.calls["admin"])
	}
}

// TestIAMDiscoveryPrefersCustomCertificateDomains verifies TLS routing matches the configured warehouse certificate.
func TestIAMDiscoveryPrefersCustomCertificateDomains(t *testing.T) {
	for _, provisioned := range []bool{false, true} {
		iam, stub := iamTestClient()
		if provisioned {
			iam.Workgroup, iam.Cluster, iam.DBUser = "", "cluster", "reader"
			stub.clusters[0].CustomDomainName = aws.String("redshift.internal.example.com")
		} else {
			stub.workgroup.CustomDomainName = aws.String("redshift.internal.example.com")
		}
		credentials, err := iam.Resolve(context.Background(), "admin")
		require.NoError(t, err)
		assert.Equal(t, "redshift.internal.example.com", credentials.Host)
	}
}

// TestIAMARNResolution covers pagination, unknown workgroups, invalid ARN kinds, and API failures.
func TestIAMARNResolution(t *testing.T) {
	iam, _ := iamTestClient()
	iam.Workgroup = "arn:aws:redshift-serverless:eu-central-1:123456789012:workgroup/uuid"
	_, err := iam.Resolve(context.Background(), "admin")
	require.NoError(t, err)
	assert.Equal(t, "warehouse", iam.name)
	for _, mode := range []string{"invalid", "absent", "list"} {
		iam, stub := iamTestClient()
		iam.Workgroup = "arn:aws:redshift-serverless:eu-central-1:123456789012:workgroup/uuid"
		switch mode {
		case "invalid":
			iam.Workgroup = "arn:aws:iam::123456789012:role/reader"
		case "absent":
			stub.absent = true
		case "list":
			stub.fail = "list"
		}
		_, err := iam.Resolve(context.Background(), "admin")
		require.Error(t, err)
	}
}

// TestIAMDiscoveryAndCredentialFailures rejects incomplete endpoints and unusable credentials without caching them.
func TestIAMDiscoveryAndCredentialFailures(t *testing.T) {
	for _, mode := range []string{"workgroup", "cluster", "serverless credentials", "cluster credentials", "missing workgroup", "missing endpoint", "missing cluster", "missing cluster endpoint", "host", "port", "username", "password", "expiration", "expired"} {
		iam, stub := iamTestClient()
		switch mode {
		case "cluster", "cluster credentials":
			iam.Workgroup, iam.Cluster, iam.DBUser = "", "cluster", "reader"
			stub.fail = mode
		case "missing cluster":
			iam.Workgroup, iam.Cluster = "", "cluster"
			stub.clusters = nil
		case "missing cluster endpoint":
			iam.Workgroup, iam.Cluster = "", "cluster"
			stub.clusters[0].Endpoint = nil
		case "missing workgroup":
			stub.workgroup = nil
		case "missing endpoint":
			stub.workgroup.Endpoint = nil
		case "host":
			stub.workgroup.Endpoint.Address = aws.String("")
		case "port":
			stub.workgroup.Endpoint.Port = aws.Int32(70000)
		case "username":
			stub.username = ""
		case "password":
			stub.password = ""
		case "expiration":
			stub.expiration = nil
		case "expired":
			expired := time.Now().Add(-time.Minute)
			stub.expiration = &expired
		default:
			stub.fail = mode
		}
		_, err := iam.Resolve(context.Background(), "admin")
		require.Error(t, err, mode)
		assert.Empty(t, iam.cache)
	}
}
