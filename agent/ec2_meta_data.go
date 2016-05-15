package agent

import (
	"fmt"

	"github.com/AdRoll/goamz/aws"
	"github.com/buildkite/agent/logger"
)

type EC2MetaData struct {
}

func (e EC2MetaData) Get() map[string]string {
	metaData := make(map[string]string)

	fetchMetaData(metaData, "instance-id")
	fetchMetaData(metaData, "instance-type")
	fetchMetaData(metaData, "ami-id")

	return metaData
}

func fetchMetaData(metaData map[string]string, propertyName string) {
	value, err := aws.GetMetaData(propertyName)
	if err != nil {
		logger.Error(fmt.Sprintf("Fetching EC2 %s failed: %s", propertyName, err.Error()))
	} else {
		metaData["aws:"+propertyName] = string(value)
	}
}
