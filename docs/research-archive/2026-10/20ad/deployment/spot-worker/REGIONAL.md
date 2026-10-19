# Regional configuration and fleet prerequisites

`regional-stack.json` is a reusable CloudFormation template. It creates an instance IAM role/profile, the requested inbound security rules and a Spot launch template. It does not launch EC2 instances, deploy a fleet or create model bucket replicas.

For each enabled region, select an existing VPC and public subnets, verify the regional `SpeekToMe` key pair, and select a tested NVIDIA-driver AMI with AWS CLI installed. Use the ARM Ubuntu 24.04 T4G image only for g5g; x86 requires its own regional AMI. Review IP CIDRs if the main server or development PC changes address. The role reads only the selected artifact bucket and the token parameter in its separately specified region.

Example Oregon deployment (configuration resources only):

```bash
aws cloudformation deploy --region us-west-2 --stack-name speektome-worker-config \
  --template-file regional-stack.json --capabilities CAPABILITY_IAM \
  --parameter-overrides VpcId=vpc-REDACTED ImageId=ami-REDACTED \
    InstanceType=g5g.xlarge ArtifactBucket=speektome-inference-ACCOUNT_ID-us-east-1 \
    ArtifactRegion=us-east-1 TokenRegion=us-east-1
```

Use stack outputs for the launch template ID and security group. Place instances in public subnets with an Internet Gateway route when using this prototype's public endpoints. The template creates its own role/profile and SG instead of importing the manually created Oregon resources. Do not deploy it blindly against those resources expecting adoption.

For a regional model mirror, create a private, encrypted bucket with public access blocked and versioning/lifecycle rules matching the source. Seed the current release keys, preserving names; verify object checksums/sizes. Set `ArtifactBucket` and `ArtifactRegion` to that mirror. The token can remain in SSM us-east-1; do not replicate token values into S3. Future ongoing S3 replication requires destination configuration and a replication role, and existing objects require a seed/batch operation. No regional mirrors or replication rules have been deployed yet.

`user-data.py` and the regional template fetch bootstrap via the EC2 IAM role, without an expiring presigned URL. The one-off launcher also now uses this method. Both the bootstrap and manifest use immutable hash keys. The currently running worker's original launch recipe is retained for provenance.

Region prerequisites observed on 2026-10-06: G/VT Spot quotas are 8 vCPUs in us-east-1, us-east-2, us-west-2 and ca-central-1; us-west-1 has zero. ca-west-1 is not enabled. Recheck before launching. Eight unused vCPUs permit two g5g.xlarge workers during replacement; larger instances or other Spot usage need additional quota.

## Boot timings and remaining controller work

Measured original clean launch: 17:38:56 UTC launch request, 17:40:38 bootstrap entry, 17:43:01 ready/cloud-init finished. Therefore 102 seconds before script entry, 143 seconds in bootstrap, 245 seconds overall. This is one successful boot, not a bound on Spot availability or startup. The updated bootstrap omits the unused worker-side ffmpeg package; cold boot of that revision has not been benchmarked. ffmpeg is still needed on the PC to generate test inputs. Actual model readiness is required before routing.

To maintain one worker, use a regional Auto Scaling Group with minimum/desired 1, maximum 2 and Capacity Rebalancing, plus application readiness launch hooks and failure handling. Keep spare quota for replacement overlap. If a MixedInstancesPolicy is added, configure Spot allocation at the ASG policy level and adjust the launch-template market options as required; keep On-Demand fallback disabled unless separately authorized. A multi-region controller must coordinate regional groups and deduplicate replacement requests. Inactive regional groups must allow minimum zero and have desired zero; only the selected active region maintains desired one. After a replacement becomes ready in another region, retire the previous regional target. Do not let several independent regional groups each assume they must keep one worker if the global target is one.

The main server should own a worker registry (instance ID, region, endpoint, certificate trust, health, capacity, load and draining state). Discover project-tagged instances, authenticate readiness/load polls and select each new stream by available capacity and acceptable latency. Reserve capacity atomically; respect worker 429 admission responses. Keep the entire WebSocket on its selected worker. Gateway readiness is authenticated, so a standard unauthenticated load balancer health check against `/ready` would receive 401.

Listen for both rebalance recommendations and interruption notices using regional EventBridge forwarding, with worker IMDS polling as a second detection path. Begin replacement on rebalance; on final interruption notice stop assigning new streams to that worker and reconnect/replay affected utterances on a ready replacement. Reconcile after every action and after controller restarts to avoid duplicate workers.

Certificate enrollment/trust distribution, main-server bearer-token integration, worker registration, event handling, application-health replacement, routing and audio replay remain to implement. There is currently no automatic fleet or load-balancer controller. One active worker plus on-warning replacement can have a service gap; continuous failover requires another ready worker and reconnect/replay support.
