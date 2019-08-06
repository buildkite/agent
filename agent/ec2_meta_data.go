package agent

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
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

	instanceId, err := c.GetMetadata("instance-id")
	if err != nil {
		return metaData, err
	}

	metaData["aws:instance-id"] = string(instanceId)

	instanceType, err := c.GetMetadata("instance-type")
	if err != nil {
		return metaData, err
	}
	metaData["aws:instance-type"] = string(instanceType)

	amiId, err := c.GetMetadata("ami-id")
	if err != nil {
		return metaData, err
	}
	metaData["aws:ami-id"] = string(amiId)

	return metaData, nil
}

func newAWSClient() (*ec2metadata.EC2Metadata, error) {
	sess, err := awsSession()
	if err != nil {
		return &ec2metadata.EC2Metadata{}, err
	}

	return ec2metadata.New(sess), nil
}
