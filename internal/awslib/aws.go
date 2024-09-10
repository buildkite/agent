package awslib

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

var awsSess *session.Session

// Region detects the current AWS region where the agent might be running
// using the env but also the local instance metadata if available.
func Region() (string, error) {
	if r := os.Getenv("AWS_REGION"); r != "" {
		return r, nil
	}

	if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
		return r, nil
	}

	// The metadata service seems to want a session
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("us-east-1"),
	})
	if err != nil {
		return "", err
	}

	meta := ec2metadata.New(sess)
	if meta.Available() {
		return meta.Region()
	}

	return "", aws.ErrMissingRegion
}

// Session returns a singleton Session, creating a new Session for the
// current region if not created previously.
func Session() (*session.Session, error) {
	region, err := Region()
	if err != nil {
		return nil, err
	}

	if awsSess == nil {
		awsSess, err = session.NewSession(&aws.Config{
			Region: aws.String(region),
		})
		if err != nil {
			return nil, err
		}
	}

	return awsSess, nil
}
