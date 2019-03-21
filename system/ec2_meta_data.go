package system

import (
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

func EC2MetaData(sess *session.Session) (map[string]string, error) {
	metaData := make(map[string]string)
	ec2metadataClient := ec2metadata.New(sess)

	instanceId, err := ec2metadataClient.GetMetadata("instance-id")
	if err != nil {
		return metaData, err
	}

	metaData["aws:instance-id"] = string(instanceId)

	instanceType, err := ec2metadataClient.GetMetadata("instance-type")
	if err != nil {
		return metaData, err
	}
	metaData["aws:instance-type"] = string(instanceType)

	amiId, err := ec2metadataClient.GetMetadata("ami-id")
	if err != nil {
		return metaData, err
	}
	metaData["aws:ami-id"] = string(amiId)

	return metaData, nil
}
