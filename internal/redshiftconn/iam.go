package redshiftconn

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/arn"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
)

// ServerlessAPI supplies endpoint discovery and temporary IAM SQL credentials.
type ServerlessAPI interface {
	// GetWorkgroup discovers endpoint settings from a workgroup name.
	GetWorkgroup(context.Context, *redshiftserverless.GetWorkgroupInput, ...func(*redshiftserverless.Options)) (*redshiftserverless.GetWorkgroupOutput, error)
	// ListWorkgroups resolves workgroup ARN UUIDs to the names required by credential APIs.
	ListWorkgroups(context.Context, *redshiftserverless.ListWorkgroupsInput, ...func(*redshiftserverless.Options)) (*redshiftserverless.ListWorkgroupsOutput, error)
	// GetCredentials obtains database-specific temporary SQL authentication.
	GetCredentials(context.Context, *redshiftserverless.GetCredentialsInput, ...func(*redshiftserverless.Options)) (*redshiftserverless.GetCredentialsOutput, error)
}

// ClusterAPI supplies provisioned endpoint discovery and existing-user temporary credentials.
type ClusterAPI interface {
	// DescribeClusters retrieves endpoint routing for the selected cluster.
	DescribeClusters(context.Context, *redshift.DescribeClustersInput, ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error)
	// GetClusterCredentials authenticates an existing SQL user without user creation or group assignment.
	GetClusterCredentials(context.Context, *redshift.GetClusterCredentialsInput, ...func(*redshift.Options)) (*redshift.GetClusterCredentialsOutput, error)
}

// credentialCache retains temporary secrets in memory only until shortly before expiration.
type credentialCache struct {
	// credentials includes discovered routing and temporary SQL authentication.
	credentials Credentials
	// expires marks the AWS credential expiration time.
	expires time.Time
}

// IAM lazily discovers endpoints and refreshes temporary credentials separately for each database.
type IAM struct {
	// Workgroup is a Serverless name or ARN, mutually exclusive with Cluster.
	Workgroup string
	// Cluster selects a provisioned warehouse.
	Cluster string
	// DBUser identifies an existing SQL user for provisioned authentication.
	DBUser string
	// Serverless executes the Serverless control-plane requests.
	Serverless ServerlessAPI
	// Provisioned executes provisioned cluster requests.
	Provisioned ClusterAPI
	// mu serializes lazy discovery and credential-cache updates.
	mu sync.Mutex
	// name is the discovered workgroup name used by GetCredentials.
	name string
	// host is the discovered endpoint hostname.
	host string
	// port is the discovered endpoint port.
	port uint16
	// cache stores database-specific authentication in memory.
	cache map[string]credentialCache
	// now allows deterministic expiration testing.
	now func() time.Time
}

// workgroupName resolves an ARN's workgroup UUID without claiming name/ARN import identity equivalence.
func (i *IAM) workgroupName(ctx context.Context) (string, error) {
	if !strings.HasPrefix(i.Workgroup, "arn:") {
		return i.Workgroup, nil
	}
	parsed, err := arn.Parse(i.Workgroup)
	if err != nil || parsed.Service != "redshift-serverless" || !strings.HasPrefix(parsed.Resource, "workgroup/") {
		return "", fmt.Errorf("invalid Serverless workgroup ARN")
	}
	id := strings.TrimPrefix(parsed.Resource, "workgroup/")
	input := &redshiftserverless.ListWorkgroupsInput{}
	for {
		page, err := i.Serverless.ListWorkgroups(ctx, input)
		if err != nil {
			return "", fmt.Errorf("resolve Serverless workgroup ARN: %w", err)
		}
		for _, group := range page.Workgroups {
			if aws.ToString(group.WorkgroupId) == id && aws.ToString(group.WorkgroupName) != "" {
				return aws.ToString(group.WorkgroupName), nil
			}
		}
		if aws.ToString(page.NextToken) == "" {
			return "", fmt.Errorf("workgroup ARN is not present in the authenticated account and region")
		}
		input.NextToken = page.NextToken
	}
}

// discover loads endpoint routing on the first query, not during provider configuration.
func (i *IAM) discover(ctx context.Context) error {
	if i.host != "" {
		return nil
	}
	var host string
	var port int32
	if i.Workgroup != "" {
		name, err := i.workgroupName(ctx)
		if err != nil {
			return err
		}
		result, err := i.Serverless.GetWorkgroup(ctx, &redshiftserverless.GetWorkgroupInput{WorkgroupName: aws.String(name)})
		if err != nil {
			return fmt.Errorf("discover Serverless endpoint: %w", err)
		}
		if result.Workgroup == nil || result.Workgroup.Endpoint == nil {
			return fmt.Errorf("serverless workgroup has no SQL endpoint")
		}
		i.name = name
		host, port = aws.ToString(result.Workgroup.Endpoint.Address), aws.ToInt32(result.Workgroup.Endpoint.Port)
		if custom := aws.ToString(result.Workgroup.CustomDomainName); custom != "" {
			host = custom
		}
	} else {
		result, err := i.Provisioned.DescribeClusters(ctx, &redshift.DescribeClustersInput{ClusterIdentifier: aws.String(i.Cluster)})
		if err != nil {
			return fmt.Errorf("discover cluster endpoint: %w", err)
		}
		if len(result.Clusters) != 1 || result.Clusters[0].Endpoint == nil {
			return fmt.Errorf("cluster discovery did not return exactly one SQL endpoint")
		}
		host, port = aws.ToString(result.Clusters[0].Endpoint.Address), aws.ToInt32(result.Clusters[0].Endpoint.Port)
		if custom := aws.ToString(result.Clusters[0].CustomDomainName); custom != "" {
			host = custom
		}
	}
	if host == "" || port < 1 || port > 65535 {
		return fmt.Errorf("AWS returned an invalid SQL endpoint")
	}
	i.host, i.port = host, uint16(port)
	return nil
}

// Resolve returns credentials valid for a new connection, refreshing before expiry and never creating users.
func (i *IAM) Resolve(ctx context.Context, database string) (Credentials, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if err := i.discover(ctx); err != nil {
		return Credentials{}, err
	}
	now := time.Now
	if i.now != nil {
		now = i.now
	}
	if cached, ok := i.cache[database]; ok && now().Add(time.Minute).Before(cached.expires) {
		return cached.credentials, nil
	}
	credentials := Credentials{Host: i.host, Port: i.port}
	var expiration *time.Time
	if i.Workgroup != "" {
		result, err := i.Serverless.GetCredentials(ctx, &redshiftserverless.GetCredentialsInput{WorkgroupName: aws.String(i.name), DbName: aws.String(database), DurationSeconds: aws.Int32(900)})
		if err != nil {
			return Credentials{}, fmt.Errorf("get Serverless SQL credentials: %w", err)
		}
		credentials.Username, credentials.Password, expiration = aws.ToString(result.DbUser), aws.ToString(result.DbPassword), result.Expiration
	} else {
		result, err := i.Provisioned.GetClusterCredentials(ctx, &redshift.GetClusterCredentialsInput{ClusterIdentifier: aws.String(i.Cluster), DbUser: aws.String(i.DBUser), DbName: aws.String(database), AutoCreate: aws.Bool(false), DurationSeconds: aws.Int32(900)})
		if err != nil {
			return Credentials{}, fmt.Errorf("get cluster SQL credentials: %w", err)
		}
		credentials.Username, credentials.Password, expiration = aws.ToString(result.DbUser), aws.ToString(result.DbPassword), result.Expiration
	}
	if credentials.Username == "" || credentials.Password == "" || expiration == nil || !now().Before(*expiration) {
		return Credentials{}, fmt.Errorf("AWS returned incomplete or expired SQL credentials")
	}
	if i.cache == nil {
		i.cache = map[string]credentialCache{}
	}
	i.cache[database] = credentialCache{credentials: credentials, expires: *expiration}
	return credentials, nil
}
