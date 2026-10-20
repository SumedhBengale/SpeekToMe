package orchestrator

import (
	"context"
	"encoding/json"
	log "speektome-server/privacylog"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/sqs"
)

// Each AWS region delivers its events into its own SQS queue. No assumption
// about unsupported cross-region SQS targeting is needed.
func (o *Controller) StartEvents() {
	for _, r := range o.cfg.Regions {
		if r.EventQueueURL != "" {
			go o.consumeEvents(o.ctx, r)
		}
	}
}
func (o *Controller) consumeEvents(ctx context.Context, r RegionConfig) {
	s, e := awsSession(r.Region)
	if e != nil {
		log.Printf("event session: %v", e)
		return
	}
	client := sqs.New(s)
	for ctx.Err() == nil {
		result, e := client.ReceiveMessageWithContext(ctx, &sqs.ReceiveMessageInput{QueueUrl: aws.String(r.EventQueueURL), MaxNumberOfMessages: aws.Int64(10), WaitTimeSeconds: aws.Int64(20), VisibilityTimeout: aws.Int64(60)})
		if e != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("fleet events %s: %v", r.Region, e)
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, msg := range result.Messages {
			var event AWSEventBridgeEvent
			e = json.Unmarshal([]byte(aws.StringValue(msg.Body)), &event)
			if e == nil {
				if event.Region != r.Region {
					e = errInvalidEvent
				} else {
					e = o.ProcessEvent(event)
				}
			}
			// Invalid messages are discarded; unknown worker messages are retried so a
			// warning cannot be lost in a race with launch/discovery. Queue DLQ bounds it.
			if e == nil || e == errInvalidEvent || event.Source == "" {
				_, err := client.DeleteMessageWithContext(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(r.EventQueueURL), ReceiptHandle: msg.ReceiptHandle})
				if err != nil {
					log.Printf("event ack: %v", err)
				}
			} else {
				log.Printf("event pending %s: %v", aws.StringValue(msg.MessageId), e)
			}
		}
	}
}
