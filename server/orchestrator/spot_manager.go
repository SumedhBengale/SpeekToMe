package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// SpotPoolCandidate holds discovered price and location for an instance type.
type SpotPoolCandidate struct {
	Region           string  `json:"region"`
	AvailabilityZone string  `json:"availability_zone"`
	InstanceType     string  `json:"instance_type"`
	SpotPriceUSD     float64 `json:"spot_price_usd"`
	LatencyMs        float64 `json:"latency_ms"`
}

// SpotManager defines the cloud interface for managing Spot instances.
type SpotManager interface {
	FindCheapestValidPool(ctx context.Context, validRegions []RegionLatency, instanceTypes []string) (*SpotPoolCandidate, error)
	LaunchSpotWorker(ctx context.Context, pool *SpotPoolCandidate, amiID string, keyName string, securityGroup string, userData string) (string, error)
	TerminateWorker(ctx context.Context, region string, instanceID string) error
	DescribeWorkerIP(ctx context.Context, region string, instanceID string) (string, error)
}

// AWSSpotManager implements SpotManager using the AWS Go SDK.
type AWSSpotManager struct{}

func NewAWSSpotManager() *AWSSpotManager {
	return &AWSSpotManager{}
}

// FindCheapestValidPool queries real-time Spot prices across all valid low-latency regions.
func (m *AWSSpotManager) FindCheapestValidPool(ctx context.Context, validRegions []RegionLatency, instanceTypes []string) (*SpotPoolCandidate, error) {
	if len(validRegions) == 0 {
		return nil, fmt.Errorf("no low-latency regions available (<80ms)")
	}
	if len(instanceTypes) == 0 {
		instanceTypes = []string{"g6f.large"}
	}

	var bestPool *SpotPoolCandidate
	lowestPrice := float64(1000.0)

	for _, regLat := range validRegions {
		sess, err := session.NewSession(&aws.Config{
			Region: aws.String(regLat.Region),
		})
		if err != nil {
			log.Printf("[SPOT DISCOVERY] Failed to create AWS session for region %s: %v", regLat.Region, err)
			continue
		}

		client := ec2.New(sess)
		awsTypes := make([]*string, len(instanceTypes))
		for i, t := range instanceTypes {
			awsTypes[i] = aws.String(t)
		}

		// Query current spot price history
		input := &ec2.DescribeSpotPriceHistoryInput{
			InstanceTypes:       awsTypes,
			ProductDescriptions: []*string{aws.String("Linux/UNIX")},
			StartTime:           aws.Time(time.Now().Add(-1 * time.Hour)),
			MaxResults:          aws.Int64(10),
		}

		out, err := client.DescribeSpotPriceHistoryWithContext(ctx, input)
		if err != nil {
			log.Printf("[SPOT DISCOVERY] DescribeSpotPriceHistory failed in %s: %v", regLat.Region, err)
			continue
		}

		for _, item := range out.SpotPriceHistory {
			if item.SpotPrice == nil || item.AvailabilityZone == nil || item.InstanceType == nil {
				continue
			}
			price, perr := strconv.ParseFloat(*item.SpotPrice, 64)
			if perr != nil {
				continue
			}

			if price < lowestPrice {
				lowestPrice = price
				bestPool = &SpotPoolCandidate{
					Region:           regLat.Region,
					AvailabilityZone: *item.AvailabilityZone,
					InstanceType:     *item.InstanceType,
					SpotPriceUSD:     price,
					LatencyMs:        regLat.LatencyMs,
				}
			}
		}
	}

	if bestPool == nil {
		return nil, fmt.Errorf("could not find any active Spot price data in candidate regions")
	}

	log.Printf("[SPOT DISCOVERY] Selected cheapest pool: %s (%s) @ $%.4f/hr [RTT: %.1fms]",
		bestPool.InstanceType, bestPool.AvailabilityZone, bestPool.SpotPriceUSD, bestPool.LatencyMs)

	return bestPool, nil
}

// LaunchSpotWorker provisions an EC2 Spot instance in the chosen pool.
func (m *AWSSpotManager) LaunchSpotWorker(ctx context.Context, pool *SpotPoolCandidate, amiID string, keyName string, securityGroup string, userData string) (string, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(pool.Region),
	})
	if err != nil {
		return "", fmt.Errorf("failed to init session for %s: %w", pool.Region, err)
	}

	client := ec2.New(sess)
	input := &ec2.RunInstancesInput{
		ImageId:      aws.String(amiID),
		InstanceType: aws.String(pool.InstanceType),
		MinCount:     aws.Int64(1),
		MaxCount:     aws.Int64(1),
		InstanceMarketOptions: &ec2.InstanceMarketOptionsRequest{
			MarketType: aws.String("spot"),
			SpotOptions: &ec2.SpotMarketOptions{
				SpotInstanceType: aws.String("one-time"),
			},
		},
		Placement: &ec2.Placement{
			AvailabilityZone: aws.String(pool.AvailabilityZone),
		},
		TagSpecifications: []*ec2.TagSpecification{
			{
				ResourceType: aws.String("instance"),
				Tags: []*ec2.Tag{
					{Key: aws.String("Name"), Value: aws.String("SpeekToMe-Nemotron-ASR-Worker")},
					{Key: aws.String("Role"), Value: aws.String("asr-streaming-worker")},
				},
			},
		},
	}

	if keyName == "" {
		keyName = "SpeekToMe"
	}
	input.KeyName = aws.String(keyName)

	if securityGroup != "" && strings.HasPrefix(securityGroup, "sg-") {
		input.SecurityGroupIds = []*string{aws.String(securityGroup)}
	} else {
		// Auto-lookup speektome-spot-worker-sg in target region
		sgOut, sgErr := client.DescribeSecurityGroupsWithContext(ctx, &ec2.DescribeSecurityGroupsInput{
			Filters: []*ec2.Filter{
				{Name: aws.String("group-name"), Values: []*string{aws.String("speektome-spot-worker-sg")}},
			},
		})
		if sgErr == nil && len(sgOut.SecurityGroups) > 0 {
			input.SecurityGroupIds = []*string{sgOut.SecurityGroups[0].GroupId}
		}
	}
	if userData != "" {
		input.UserData = aws.String(userData)
	}

	res, err := client.RunInstancesWithContext(ctx, input)
	if err != nil {
		return "", fmt.Errorf("RunInstances spot request failed in %s: %w", pool.Region, err)
	}

	if len(res.Instances) == 0 || res.Instances[0].InstanceId == nil {
		return "", fmt.Errorf("no instance returned in RunInstances response")
	}

	instanceID := *res.Instances[0].InstanceId
	log.Printf("[SPOT LAUNCH] Provisioned Spot instance %s in %s (%s)", instanceID, pool.Region, pool.AvailabilityZone)
	return instanceID, nil
}

// TerminateWorker terminates an EC2 instance.
func (m *AWSSpotManager) TerminateWorker(ctx context.Context, region string, instanceID string) error {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return err
	}
	client := ec2.New(sess)
	_, err = client.TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []*string{aws.String(instanceID)},
	})
	log.Printf("[SPOT TERMINATE] Terminated instance %s in %s (err=%v)", instanceID, region, err)
	return err
}

// DescribeWorkerIP retrieves the public or private IP of a running instance.
func (m *AWSSpotManager) DescribeWorkerIP(ctx context.Context, region string, instanceID string) (string, error) {
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(region),
	})
	if err != nil {
		return "", err
	}
	client := ec2.New(sess)
	out, err := client.DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []*string{aws.String(instanceID)},
	})
	if err != nil {
		return "", err
	}
	if len(out.Reservations) > 0 && len(out.Reservations[0].Instances) > 0 {
		inst := out.Reservations[0].Instances[0]
		if inst.PublicIpAddress != nil && *inst.PublicIpAddress != "" {
			return *inst.PublicIpAddress, nil
		}
		if inst.PrivateIpAddress != nil && *inst.PrivateIpAddress != "" {
			return *inst.PrivateIpAddress, nil
		}
	}
	return "", fmt.Errorf("no IP address assigned yet for %s", instanceID)
}
