package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/ratelimit"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/redshiftdata"
	"github.com/aws/aws-sdk-go-v2/service/redshiftserverless"
	"github.com/netcheck-de/terraform-provider-redshift/internal/redshiftconn"
	dataapi "github.com/netcheck-de/terraform-provider-redshift/internal/redshiftdata"
	"github.com/netcheck-de/terraform-provider-redshift/internal/sqlclient"
)

// findDatashareARN matches the producer identity against inbound and outbound datashares visible to the account.
// DescribeDataSharesForConsumer omits same-account shares, so the general listing is required.
func findDatashareARN(ctx context.Context, api redshift.DescribeDataSharesAPIClient, source shareSource) (string, error) {
	pages := redshift.NewDescribeDataSharesPaginator(api, &redshift.DescribeDataSharesInput{}, func(options *redshift.DescribeDataSharesPaginatorOptions) {
		options.StopOnDuplicateToken = true
	})
	var found string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return "", fmt.Errorf("describe datashares: %w", err)
		}
		for _, share := range page.DataShares {
			value := aws.ToString(share.DataShareArn)
			observed, err := parseShare(value)
			if err != nil || observed != source {
				continue
			}
			if found != "" && found != value {
				return "", fmt.Errorf("multiple datashare ARNs match producer %s/%s/%s", source.Account, source.Namespace, source.Name)
			}
			found = value
		}
	}
	if found == "" {
		return "", fmt.Errorf("no consumer-visible datashare ARN matches producer %s/%s/%s", source.Account, source.Namespace, source.Name)
	}
	return found, nil
}

// dataAPIRetryableCodes are Data API errors raised before a statement starts, so retrying cannot run SQL twice.
// ThrottlingException is already a throttle code in the standard retryer; listing it keeps it retryable regardless.
var dataAPIRetryableCodes = []string{"ThrottlingException", "ActiveStatementsExceededException"}

// awsConfig loads the AWS SDK configuration shared by every AWS client the provider builds.
func (data providerModel) awsConfig(ctx context.Context) (aws.Config, error) {
	var options []func(*config.LoadOptions) error
	if !data.Region.IsNull() {
		options = append(options, config.WithRegion(data.Region.ValueString()))
	}
	if !data.Profile.IsNull() {
		options = append(options, config.WithSharedConfigProfile(data.Profile.ValueString()))
	}
	if !data.MaxRetries.IsNull() {
		// max_retries counts retries like the AWS provider; the SDK counts attempts.
		options = append(options, config.WithRetryMaxAttempts(int(data.MaxRetries.ValueInt64())+1))
	}
	if !data.RetryMode.IsNull() {
		options = append(options, config.WithRetryMode(aws.RetryMode(data.RetryMode.ValueString())))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return cfg, err
	}
	mode, attempts := cfg.RetryMode, cfg.RetryMaxAttempts
	cfg.Retryer = func() aws.Retryer { return newRetryer(mode, attempts) }
	return cfg, nil
}

// newRetryer builds the SDK retryer for the resolved mode and attempt limit without the SDK's client-side retry quota.
// That quota is shared by every call of a client and refilled only by successes, so sustained throttling would stop
// retries long before max_retries; the AWS provider disables it for the same reason.
func newRetryer(mode aws.RetryMode, attempts int) aws.Retryer {
	standard := func(options *retry.StandardOptions) {
		options.RateLimiter = ratelimit.None
		if attempts != 0 {
			options.MaxAttempts = attempts
		}
	}
	if mode == aws.RetryModeAdaptive {
		return retry.NewAdaptiveMode(func(options *retry.AdaptiveModeOptions) {
			options.StandardOptions = append(options.StandardOptions, standard)
		})
	}
	return retry.NewStandard(standard)
}

// datashareARN lazily loads AWS credentials only when a shared-database lookup needs producer metadata.
func (data providerModel) datashareARN(ctx context.Context, source shareSource) (string, error) {
	cfg, err := data.awsConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("configure datashare discovery: %w", err)
	}
	if cfg.Region == "" {
		return "", fmt.Errorf("shared-database datashare ARN discovery requires an AWS region and credentials, including for direct SQL connections")
	}
	return findDatashareARN(ctx, redshift.NewFromConfig(cfg), source)
}

// sqlClient constructs the selected adapter lazily; AWS configuration is unnecessary for password connections.
func (data providerModel) sqlClient(ctx context.Context, version string) (sqlclient.Client, error) {
	settings, err := data.transport(version)
	if err != nil {
		return nil, err
	}
	var direct *redshiftconn.Client
	if data.Connection != nil {
		connection := data.Connection
		direct = &redshiftconn.Client{
			Credentials:     redshiftconn.Credentials{Host: connection.Host.ValueString(), Username: connection.Username.ValueString(), Password: connection.Password.ValueString()},
			CACertFile:      connection.CACertFile.ValueString(),
			SSLMode:         connection.SSLMode.ValueString(),
			Timeout:         settings.queryTimeout,
			ConnectTimeout:  settings.connectTimeout,
			ApplicationName: settings.applicationName,
		}
		if !connection.Port.IsNull() && !connection.Port.IsUnknown() {
			direct.Credentials.Port = uint16(connection.Port.ValueInt64())
		}
		if connection.IAM == nil {
			return direct, nil
		}
	}
	cfg, err := data.awsConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("configure AWS client: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("set region or provide it through the AWS SDK configuration")
	}
	if direct != nil {
		iam := data.Connection.IAM
		direct.IAM = &redshiftconn.IAM{Workgroup: iam.Workgroup.ValueString(), Cluster: iam.ClusterIdentifier.ValueString(), DBUser: iam.DBUser.ValueString(), Serverless: redshiftserverless.NewFromConfig(cfg), Provisioned: redshift.NewFromConfig(cfg)}
		return direct, nil
	}
	api := redshiftdata.NewFromConfig(cfg, func(options *redshiftdata.Options) {
		options.Retryer = retry.AddWithErrorCodes(options.Retryer, dataAPIRetryableCodes...)
	})
	return &dataapi.Client{
		API:           api,
		Workgroup:     data.Workgroup.ValueString(),
		Cluster:       data.ClusterIdentifier.ValueString(),
		DBUser:        data.DBUser.ValueString(),
		SecretARN:     data.SecretARN.ValueString(),
		StatementName: settings.applicationName,
		Timeout:       settings.queryTimeout,
		Poll:          dataapi.DefaultPoll,
	}, nil
}
