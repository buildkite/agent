package agent

import (
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

var awsSess *session.Session

// The aws sdk relies on being given a region, which is a breaking change for us
// This applies a heuristic that detects where the agent might be based on the env
// but also the local isntance metadata if available
func awsRegion() (string, error) {
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

func awsSession() (*session.Session, error) {
	region, err := awsRegion()
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
