package agent

import (
	"github.com/AdRoll/goamz/aws"
)

type EC2MetaData struct {
}

func (e EC2MetaData) Get() (map[string]string, error) {
	metaData := make(map[string]string)

	instanceId, err := aws.GetMetaData("instance-id")
	if err != nil {
		return metaData, err
	}
	metaData["aws:instance-id"] = string(instanceId)

	instanceType, err := aws.GetMetaData("instance-type")
	if err != nil {
		return metaData, err
	}
	metaData["aws:instance-type"] = string(instanceType)

	amiId, err := aws.GetMetaData("ami-id")
	if err != nil {
		return metaData, err
	}
	metaData["aws:ami-id"] = string(amiId)

	return metaData, nil
}
