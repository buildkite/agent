package agent

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/service/ec2"
)

type EC2Tags struct {
}

func (e EC2Tags) Get() (map[string]string, error) {
	tags := make(map[string]string)

	ec2metadataClient := ec2metadata.New(nil)

	// Find out the region of the current instance
	instanceRegion, err := ec2metadataClient.Region()
	if err != nil {
		return tags, err
	}

	// Grab the current instances id
	instanceId, err := ec2metadataClient.GetMetadata("instance-id")
	if err != nil {
		return tags, err
	}

	// Create an ec2 client (not the lack of credentials, we pass nothing
	// so it looks at the current systems credentials)
	ec2Client := ec2.New(&aws.Config{Region: aws.String(instanceRegion)})

	// Describe the tags of the current instance
	params := &ec2.DescribeTagsInput{
		Filters: []*ec2.Filter{
			{
				Name: aws.String("resource-id"),
				Values: []*string{
					aws.String(instanceId),
				},
			},
		},
	}
	resp, err := ec2Client.DescribeTags(params)
	if err != nil {
		return tags, err
	}

	// Collect the tags
	for _, tag := range resp.Tags {
		tags[*tag.Key] = *tag.Value
	}

	return tags, nil
}
