package agent

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type EC2Tags struct {
}

func (e EC2Tags) Get() (map[string]string, error) {
	sess, err := awsSession()
	if err != nil {
		return nil, err
	}

	tags := make(map[string]string)
	ec2metadataClient := ec2metadata.New(sess)

	// Grab the current instances id
	instanceId, err := ec2metadataClient.GetMetadata("instance-id")
	if err != nil {
		return tags, err
	}

	svc := ec2.New(sess)

	// Describe the tags of the current instance
	resp, err := svc.DescribeTags(&ec2.DescribeTagsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("resource-id"),
				Values: []*string{
					aws.String(instanceId),
				},
			},
		},
	})
	if err != nil {
		return tags, err
	}

	// Collect the tags
	for _, tag := range resp.Tags {
		tags[*tag.Key] = *tag.Value
	}

	return tags, nil
}
