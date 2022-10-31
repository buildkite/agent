package agent

import (
	"errors"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/roko"
)

type Autoscaling struct {
	AutoscalingGroupName string
}

// Determines ASG name from tags on EC2 instance
func NewAutoScalingFromTags() (*Autoscaling, error) {
	tags, err := (EC2Tags{}).Get()
	if err != nil {
		return nil, err
	}

	asgName, exists := tags["aws:autoscaling:groupName"]
	if !exists {
		return nil, errors.New("ASG not tagged")
	}

	return &Autoscaling{
		AutoscalingGroupName: asgName,
	}, nil
}

func (a *Autoscaling) CompleteLifecycleAction(
	l logger.Logger,
	lifecycleHookName, actionResult string,
) error {
	session, err := awsSession()
	if err != nil {
		return err
	}

	svc := autoscaling.New(session)
	input := &autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  aws.String(a.AutoscalingGroupName),
		LifecycleActionResult: aws.String(actionResult),
		LifecycleHookName:     aws.String(lifecycleHookName),
	}

	return roko.NewRetrier(
		roko.WithMaxAttempts(10),
		roko.WithStrategy(roko.Exponential(1*time.Second, time.Second)),
	).Do(func(retrier *roko.Retrier) error {
		_, err := svc.CompleteLifecycleAction(input)
		if err != nil {
			// TODO: maybe put a heartbeat in here
			if aerr, ok := err.(awserr.Error); ok {
				switch aerr.Code() {
				case autoscaling.ErrCodeResourceContentionFault:
					l.Info("%s: %e", autoscaling.ErrCodeResourceContentionFault, aerr)
				default:
					l.Info("%e", aerr)
				}
			} else {
				// Print the error, cast err to awserr.Error to get the Code and
				// Message from an error.
				l.Info("%e", aerr)
			}
			return err
		}

		l.Info("Lifecycle Hook Complete Action Completed")

		return nil
	})
}
