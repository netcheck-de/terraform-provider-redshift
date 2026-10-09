package provider

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
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

// datashareARN lazily loads AWS credentials only when a shared-database lookup needs producer metadata.
func (data providerModel) datashareARN(ctx context.Context, source shareSource) (string, error) {
	var options []func(*config.LoadOptions) error
	if !data.Region.IsNull() {
		options = append(options, config.WithRegion(data.Region.ValueString()))
	}
	if !data.Profile.IsNull() {
		options = append(options, config.WithSharedConfigProfile(data.Profile.ValueString()))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return "", fmt.Errorf("configure datashare discovery: %w", err)
	}
	if cfg.Region == "" {
		return "", fmt.Errorf("shared-database datashare ARN discovery requires an AWS region and credentials, including for direct SQL connections")
	}
	return findDatashareARN(ctx, redshift.NewFromConfig(cfg), source)
}

// sqlClient constructs the selected adapter lazily; AWS configuration is unnecessary for password connections.
func (data providerModel) sqlClient(ctx context.Context) (sqlclient.Client, error) {
	var direct *redshiftconn.Client
	if data.Connection != nil {
		connection := data.Connection
		direct = &redshiftconn.Client{Credentials: redshiftconn.Credentials{Host: connection.Host.ValueString(), Username: connection.Username.ValueString(), Password: connection.Password.ValueString()}, CACertFile: connection.CACertFile.ValueString(), SSLMode: connection.SSLMode.ValueString(), Timeout: redshiftconn.DefaultTimeout}
		if !connection.Port.IsNull() && !connection.Port.IsUnknown() {
			direct.Credentials.Port = uint16(connection.Port.ValueInt64())
		}
		if connection.IAM == nil {
			return direct, nil
		}
	}
	var options []func(*config.LoadOptions) error
	if !data.Region.IsNull() {
		options = append(options, config.WithRegion(data.Region.ValueString()))
	}
	if !data.Profile.IsNull() {
		options = append(options, config.WithSharedConfigProfile(data.Profile.ValueString()))
	}
	cfg, err := config.LoadDefaultConfig(ctx, options...)
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
	return &dataapi.Client{API: redshiftdata.NewFromConfig(cfg), Workgroup: data.Workgroup.ValueString(), Cluster: data.ClusterIdentifier.ValueString(), DBUser: data.DBUser.ValueString(), SecretARN: data.SecretARN.ValueString(), Timeout: dataapi.DefaultTimeout, Poll: dataapi.DefaultPoll}, nil
}
