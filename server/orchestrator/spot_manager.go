package orchestrator

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/s3"
)

type AWSCloud struct{}

func NewAWSCloud() *AWSCloud { return &AWSCloud{} }
func awsSession(region string) (*session.Session, error) {
	return session.NewSessionWithOptions(session.Options{Config: aws.Config{Region: aws.String(region), MaxRetries: aws.Int(2)}, SharedConfigState: session.SharedConfigEnable})
}
func regional(c FleetConfig, region string) (RegionConfig, error) {
	for _, r := range c.Regions {
		if r.Region == region {
			return r, nil
		}
	}
	return RegionConfig{}, errors.New("unconfigured region")
}
func instanceWorker(i *ec2.Instance, r string) SpotWorker {
	w := SpotWorker{InstanceID: aws.StringValue(i.InstanceId), Region: r, AvailabilityZone: aws.StringValue(i.Placement.AvailabilityZone), InstanceType: aws.StringValue(i.InstanceType), LaunchedAt: aws.TimeValue(i.LaunchTime)}
	for _, nic := range i.NetworkInterfaces {
		for _, ip := range nic.Ipv6Addresses {
			if ip.Ipv6Address != nil {
				w.IPAddress = *ip.Ipv6Address
				w.EndpointURL = "https://" + net.JoinHostPort(w.IPAddress, "80")
				return w
			}
		}
	}
	return w
}
func (m *AWSCloud) Discover(ctx context.Context, c FleetConfig) (CloudSnapshot, error) {
	out := CloudSnapshot{Complete: true}
	for _, r := range c.Regions {
		s, e := awsSession(r.Region)
		if e != nil {
			return CloudSnapshot{}, e
		}
		client := ec2.New(s)
		input := &ec2.DescribeInstancesInput{Filters: []*ec2.Filter{{Name: aws.String("tag:SpeekToMeFleet"), Values: []*string{aws.String(c.FleetID)}}, {Name: aws.String("instance-state-name"), Values: aws.StringSlice([]string{"pending", "running", "stopping", "stopped", "shutting-down"})}}}
		e = client.DescribeInstancesPagesWithContext(ctx, input, func(page *ec2.DescribeInstancesOutput, last bool) bool {
			for _, res := range page.Reservations {
				for _, i := range res.Instances {
					// Do not adopt On-Demand or unrelated instance types even with matching tags.
					if aws.StringValue(i.InstanceLifecycle) != "spot" || aws.StringValue(i.InstanceType) != c.InstanceType {
						continue
					}
					w := instanceWorker(i, r.Region)
					state := aws.StringValue(i.State.Name)
					if state != "running" && state != "pending" {
						w.State = StateDraining
						w.Interruption = true
					}
					out.Workers = append(out.Workers, w)
				}
			}
			return true
		})
		if e != nil {
			return CloudSnapshot{Complete: false}, fmt.Errorf("discover %s: %w", r.Region, e)
		}
	}
	return out, nil
}
func (m *AWSCloud) Pools(ctx context.Context, c FleetConfig) ([]SpotPoolCandidate, error) {
	out := []SpotPoolCandidate{}
	for _, r := range c.Regions {
		latency := MeasureRegionLatency(ctx, r.Region, 2*time.Second)
		if !latency.Reachable {
			continue
		}
		if c.MaxRTTMs > 0 && latency.LatencyMs > c.MaxRTTMs {
			continue
		}
		s, e := awsSession(r.Region)
		if e != nil {
			return nil, e
		}
		client := ec2.New(s)
		quotes := map[string]*ec2.SpotPrice{}
		input := &ec2.DescribeSpotPriceHistoryInput{InstanceTypes: aws.StringSlice([]string{c.InstanceType}), ProductDescriptions: aws.StringSlice([]string{"Linux/UNIX"}), StartTime: aws.Time(time.Now()), MaxResults: aws.Int64(100)}
		e = client.DescribeSpotPriceHistoryPagesWithContext(ctx, input, func(page *ec2.DescribeSpotPriceHistoryOutput, last bool) bool {
			for _, q := range page.SpotPriceHistory {
				az := aws.StringValue(q.AvailabilityZone)
				if _, ok := r.Subnets[az]; !ok {
					continue
				}
				if old := quotes[az]; old == nil || aws.TimeValue(q.Timestamp).After(aws.TimeValue(old.Timestamp)) {
					quotes[az] = q
				}
			}
			return true
		})
		if e != nil {
			return nil, fmt.Errorf("prices %s: %w", r.Region, e)
		}
		for az, q := range quotes {
			price, e := strconv.ParseFloat(aws.StringValue(q.SpotPrice), 64)
			if e == nil {
				out = append(out, SpotPoolCandidate{Region: r.Region, AvailabilityZone: az, InstanceType: c.InstanceType, SpotPriceUSD: price, LatencyMs: latency.LatencyMs})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SpotPriceUSD < out[j].SpotPriceUSD })
	return out, nil
}
func (m *AWSCloud) Launch(ctx context.Context, c FleetConfig, p SpotPoolCandidate, token string) (SpotWorker, error) {
	r, e := regional(c, p.Region)
	if e != nil {
		return SpotWorker{}, &DefiniteLaunchError{e}
	}
	s, e := awsSession(p.Region)
	if e != nil {
		return SpotWorker{}, e
	}
	client := ec2.New(s)
	input := &ec2.RunInstancesInput{
		ImageId: aws.String(r.AMI), InstanceType: aws.String(c.InstanceType), MinCount: aws.Int64(1), MaxCount: aws.Int64(1), ClientToken: aws.String(token), KeyName: aws.String(c.KeyName),
		UserData:              aws.String(base64.StdEncoding.EncodeToString([]byte(r.UserData))),
		IamInstanceProfile:    &ec2.IamInstanceProfileSpecification{Name: aws.String(c.InstanceProfile)},
		MetadataOptions:       &ec2.InstanceMetadataOptionsRequest{HttpTokens: aws.String("required"), HttpEndpoint: aws.String("enabled"), HttpProtocolIpv6: aws.String("enabled"), HttpPutResponseHopLimit: aws.Int64(1)},
		InstanceMarketOptions: &ec2.InstanceMarketOptionsRequest{MarketType: aws.String("spot"), SpotOptions: &ec2.SpotMarketOptions{SpotInstanceType: aws.String("one-time"), InstanceInterruptionBehavior: aws.String("terminate"), MaxPrice: aws.String(strconv.FormatFloat(c.MaxPoolHourlyUSD, 'f', 4, 64))}},
		NetworkInterfaces:     []*ec2.InstanceNetworkInterfaceSpecification{{DeviceIndex: aws.Int64(0), SubnetId: aws.String(r.Subnets[p.AvailabilityZone]), Groups: aws.StringSlice([]string{r.SecurityGroup}), AssociatePublicIpAddress: aws.Bool(false), Ipv6AddressCount: aws.Int64(1), DeleteOnTermination: aws.Bool(true)}},
		Placement:             &ec2.Placement{AvailabilityZone: aws.String(p.AvailabilityZone)},
		BlockDeviceMappings:   []*ec2.BlockDeviceMapping{{DeviceName: aws.String("/dev/sda1"), Ebs: &ec2.EbsBlockDevice{VolumeSize: aws.Int64(c.RootVolumeGiB), VolumeType: aws.String("gp3"), Encrypted: aws.Bool(true), DeleteOnTermination: aws.Bool(true)}}},
	}
	for _, kind := range []string{"instance", "volume"} {
		input.TagSpecifications = append(input.TagSpecifications, &ec2.TagSpecification{ResourceType: aws.String(kind), Tags: []*ec2.Tag{{Key: aws.String("Name"), Value: aws.String("SpeekToMe Fleet Worker")}, {Key: aws.String("SpeekToMeFleet"), Value: aws.String(c.FleetID)}, {Key: aws.String("LaunchToken"), Value: aws.String(token)}}})
	}
	result, e := client.RunInstancesWithContext(ctx, input)
	if e != nil {
		// Server failures/timeouts are ambiguous; retain the exact client token.
		if definiteLaunchFailure(e) {
			return SpotWorker{}, &DefiniteLaunchError{e}
		}
		return SpotWorker{}, e
	}
	if len(result.Instances) != 1 {
		return SpotWorker{}, errors.New("ambiguous RunInstances response")
	}
	w := instanceWorker(result.Instances[0], p.Region)
	return w, nil
}
func (m *AWSCloud) Terminate(ctx context.Context, w SpotWorker) error {
	s, e := awsSession(w.Region)
	if e != nil {
		return e
	}
	_, e = ec2.New(s).TerminateInstancesWithContext(ctx, &ec2.TerminateInstancesInput{InstanceIds: aws.StringSlice([]string{w.InstanceID})})
	return e
}
func (m *AWSCloud) Certificate(ctx context.Context, c FleetConfig, w SpotWorker) ([]byte, error) {
	r, e := regional(c, w.Region)
	if e != nil {
		return nil, e
	}
	s, e := awsSession(r.ArtifactRegion)
	if e != nil {
		return nil, e
	}
	client := s3.New(s, &aws.Config{UseDualStack: aws.Bool(true)})
	out, e := client.GetObjectWithContext(ctx, &s3.GetObjectInput{Bucket: aws.String(r.ArtifactBucket), Key: aws.String("registration/" + w.InstanceID + "/tls.crt")})
	if e != nil {
		return nil, e
	}
	defer out.Body.Close()
	return io.ReadAll(io.LimitReader(out.Body, 32<<10))
}

// AWS returns some explicit capacity rejections as HTTP 500. These are known
// unsuccessful launches, unlike a timeout or generic internal server error.
func definiteLaunchFailure(err error) bool {
	var api awserr.Error
	if errors.As(err, &api) {
		switch api.Code() {
		case "InsufficientInstanceCapacity", "InsufficientHostCapacity", "InsufficientReservedInstanceCapacity", "MaxSpotInstanceCountExceeded", "SpotMaxPriceTooLow", "VcpuLimitExceeded", "InstanceLimitExceeded":
			return true
		}
	}
	var rf awserr.RequestFailure
	return errors.As(err, &rf) && rf.StatusCode() >= 400 && rf.StatusCode() < 500
}
