package secrets

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/aws/aws-sdk-go/service/sts"
)

type AWSSSMProviderConfig struct {
	ID            string `json:"id"`
	RoleARN       string `json:"role_arn"`
	AssumeViaOIDC bool   `json:"assume_via_oidc"`
}

type AWSSSMProvider struct {
	id   string
	ssmI *ssm.SSM
}

func NewAWSSSMProvider(id string, config AWSSSMProviderConfig) (*AWSSSMProvider, error) {
	sess, err := session.NewSession()
	if err != nil {
		return nil, fmt.Errorf("initialising AWS session: %w", err)
	}

	return &AWSSSMProvider{
		id:   id,
		ssmI: generateSSMInstance(sess, config),
	}, nil
}

func (s *AWSSSMProvider) ID() string {
	return s.id
}

func (s *AWSSSMProvider) Type() string {
	return "aws-ssm"
}

func (s *AWSSSMProvider) Fetch(key string) (string, error) {
	out, err := s.ssmI.GetParameter(&ssm.GetParameterInput{
		Name:           aws.String(key),
		WithDecryption: aws.Bool(true),
	})

	if err != nil {
		return "", fmt.Errorf("retrieving secret %s from SSM Parameter Store: %w", key, err)
	}

	return *out.Parameter.Value, nil
}

func generateSSMInstance(sess *session.Session, config AWSSSMProviderConfig) *ssm.SSM {
	if config.RoleARN == "" {
		return ssm.New(sess)
	}

	if config.AssumeViaOIDC {
		stsClient := sts.New(sess)
		sessionName := fmt.Sprintf("buildkite-agent-aws-ssm-secrets-provider-%s", os.Getenv("BUILDKITE_JOB_ID"))
		// TODO: Use BK OIDC provider instead of some rando file
		roleProvider := stscreds.NewWebIdentityRoleProviderWithOptions(stsClient, config.RoleARN, sessionName, stscreds.FetchTokenPath("/build/token"))
		creds := credentials.NewCredentials(roleProvider)
		return ssm.New(sess, &aws.Config{Credentials: creds})
	}

	creds := stscreds.NewCredentials(sess, config.RoleARN)
	return ssm.New(sess, &aws.Config{Credentials: creds})
}
