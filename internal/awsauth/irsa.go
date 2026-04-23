// Package awsauth provides AWS credential helpers, notably IRSA (IAM Roles for
// Service Accounts) for EKS pods using a projected service-account token.
package awsauth

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

// DefaultTokenPath is the EKS IRSA projected-volume token path.
const DefaultTokenPath = "/var/run/secrets/eks.amazonaws.com/serviceaccount/token"

// IRSAProvider returns an auto-refreshing credential provider when AWS_ROLE_ARN
// is set and the projected service-account token is available. If the caller
// isn't running under IRSA (no role ARN or missing token), it returns (nil, nil)
// so the default AWS credential chain is used instead.
func IRSAProvider(ctx context.Context, cfg *config.Config, log *slog.Logger) (aws.CredentialsProvider, error) {
	if cfg.S3.RoleARN == "" {
		return nil, nil
	}
	if _, err := os.Stat(DefaultTokenPath); err != nil {
		log.Warn("IRSA token file not found; falling back to default credentials", "path", DefaultTokenPath)
		return nil, nil
	}

	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.S3.Region != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.S3.Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("aws config for sts: %w", err)
	}

	stsClient := sts.NewFromConfig(awsCfg)
	provider := stscreds.NewWebIdentityRoleProvider(
		stsClient,
		cfg.S3.RoleARN,
		stscreds.IdentityTokenFile(DefaultTokenPath),
		func(o *stscreds.WebIdentityRoleOptions) {
			o.RoleSessionName = fmt.Sprintf("dumpscript-%d", time.Now().Unix())
		},
	)

	log.Info("IRSA credential provider configured", "role_arn", cfg.S3.RoleARN)
	return aws.NewCredentialsCache(provider), nil
}
