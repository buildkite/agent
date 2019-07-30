package agent

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
)

type EC2MetaData struct {
	Client *ec2metadata.EC2Metadata
}

func NewEC2MetaData() (*EC2MetaData, error) {
	sess, err := awsSession()
	if err != nil {
		return &EC2MetaData{}, err
	}

	return &EC2MetaData{
		Client: ec2metadata.New(sess),
	}, nil
}

// Takes a map of tags and meta-data paths to get, returns a map of tags and fetched values.
func (e EC2MetaData) GetPaths(paths map[string]string) (map[string]string, error) {
	result := make(map[string]string)

	for key, path := range paths {
		value, err := e.Client.GetMetadata(path)
		if err != nil {
			return nil, err
		} else {
			result[key] = value
		}
	}

	return result, nil
}

func (e EC2MetaData) Get() (map[string]string, error) {
	metaData := make(map[string]string)

	instanceId, err := e.Client.GetMetadata("instance-id")
	if err != nil {
		return metaData, err
	}

	metaData["aws:instance-id"] = string(instanceId)

	instanceType, err := e.Client.GetMetadata("instance-type")
	if err != nil {
		return metaData, err
	}
	metaData["aws:instance-type"] = string(instanceType)

	amiId, err := e.Client.GetMetadata("ami-id")
	if err != nil {
		return metaData, err
	}
	metaData["aws:ami-id"] = string(amiId)

	return metaData, nil
}
