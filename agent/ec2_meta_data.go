package agent

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/buildkite/agent/v3/internal/awslib"
)

type EC2MetaData struct {
}

// Takes a map of tags and meta-data paths to get, returns a map of tags and fetched values.
func (e EC2MetaData) GetPaths(paths map[string]string) (map[string]string, error) {
	metaData := make(map[string]string)

	c, err := newAWSClient()
	if err != nil {
		return metaData, err
	}

	for key, path := range paths {
		value, err := c.GetMetadata(path)
		if err != nil {
			return nil, err
		} else {
			metaData[key] = value
		}
	}

	return metaData, nil
}

func (e EC2MetaData) Get() (map[string]string, error) {
	metaData := make(map[string]string)

	c, err := newAWSClient()
	if err != nil {
		return metaData, err
	}

	document, err := c.GetInstanceIdentityDocument()
	if err != nil {
		return metaData, err
	}

	metaData["aws:account-id"] = document.AccountID
	metaData["aws:ami-id"] = document.ImageID
	metaData["aws:architecture"] = document.Architecture
	metaData["aws:availability-zone"] = document.AvailabilityZone
	metaData["aws:instance-id"] = document.InstanceID
	metaData["aws:instance-type"] = document.InstanceType
	metaData["aws:region"] = document.Region

	instanceLifeCycle, err := c.GetMetadata("instance-life-cycle")
	if err == nil {
		metaData["aws:instance-life-cycle"] = instanceLifeCycle
	}

	return metaData, nil
}

func newAWSClient() (*ec2metadata.EC2Metadata, error) {
	sess, err := awslib.Session()
	if err != nil {
		return &ec2metadata.EC2Metadata{}, err
	}

	return ec2metadata.New(sess), nil
}
