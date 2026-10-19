"""Launch/reuse exactly one inf2.xlarge with an explicit region/market.

Default is Ohio Spot. Virginia On-Demand is a separate, explicitly selected
mode authorized by the user. No automatic market/instance-size fallback.
AWS CLI credentials stay on the workstation and are not copied to the instance.
"""

import argparse
import json
import subprocess
from pathlib import Path

REGION = "us-east-2"
NAME = "speektome-inf2-feasibility"


def aws(*args):
    r = subprocess.run(["aws", *args, "--region", REGION, "--output", "json"],
                       capture_output=True, text=True)
    if r.returncode:
        raise RuntimeError(r.stderr.strip())
    return json.loads(r.stdout)


def main():
    global REGION
    p = argparse.ArgumentParser()
    p.add_argument("--region", choices=["us-east-1", "us-east-2"], default="us-east-2")
    p.add_argument("--market", choices=["spot", "on-demand"], default="spot")
    p.add_argument("--artifacts", default=str(Path(__file__).parent / "artifacts"))
    p.add_argument("--ami", default=None)
    p.add_argument("--key", default="SpeekToMe")
    p.add_argument("--disk-gib", type=int, default=120)
    args = p.parse_args()
    REGION = args.region
    quota_code = "L-B5D1601B" if args.market == "spot" else "L-1945791B"
    out = Path(args.artifacts)
    out.mkdir(parents=True, exist_ok=True)
    instances = aws("ec2", "describe-instances", "--filters",
                    f"Name=tag:Name,Values={NAME}",
                    "Name=instance-state-name,Values=pending,running,stopping,stopped")
    live = [i for r in instances["Reservations"] for i in r["Instances"]]
    if live:
        expected_market = "spot" if args.market == "spot" else None
        if len(live) != 1 or live[0].get("InstanceLifecycle") != expected_market or live[0]["InstanceType"] != "inf2.xlarge":
            raise SystemExit("Unexpected instance(s) with this tag; inspect rather than launching duplicates.")
        (out / "instance.json").write_text(json.dumps(live[0], indent=2))
        print(json.dumps({k: live[0].get(k) for k in ["InstanceId", "State", "PublicIpAddress", "SpotInstanceRequestId"]}, indent=2))
        return
    requests = aws("ec2", "describe-spot-instance-requests", "--filters", f"Name=tag:Name,Values={NAME}")
    pending = [r for r in requests["SpotInstanceRequests"] if r["State"] in ["open", "active"]]
    if pending:
        raise SystemExit("Existing Spot request is pending or active; inspect it before creating another.")
    quota = aws("service-quotas", "get-service-quota", "--service-code", "ec2", "--quota-code", quota_code)
    (out / "quota-current.json").write_text(json.dumps(quota, indent=2))
    if quota["Quota"]["Value"] < 4:
        raise SystemExit(f"BLOCKED: {quota['Quota']['QuotaName']} quota in {REGION} is below 4 vCPUs. See quota request artifacts.")
    if args.ami is None:
        images = aws("ec2", "describe-images", "--owners", "amazon", "--filters",
                     "Name=name,Values=Deep Learning AMI Neuron PyTorch 2.9 (Ubuntu 24.04)*",
                     "Name=architecture,Values=x86_64", "Name=state,Values=available")["Images"]
        args.ami = max(images, key=lambda x: x["CreationDate"])["ImageId"]
    groups = aws("ec2", "describe-security-groups", "--filters", f"Name=group-name,Values={NAME}")["SecurityGroups"]
    if not groups:
        vpc = aws("ec2", "describe-vpcs", "--filters", "Name=is-default,Values=true")["Vpcs"][0]["VpcId"]
        group = aws("ec2", "create-security-group", "--group-name", NAME,
                    "--description", "Nemotron Inf2 feasibility SSH from workstation only", "--vpc-id", vpc)
        groups = aws("ec2", "describe-security-groups", "--group-ids", group["GroupId"])["SecurityGroups"]
    group = groups[0]
    ip = subprocess.check_output(["curl", "-fsSL", "https://checkip.amazonaws.com"], text=True).strip() + "/32"
    if not any(p.get("FromPort") == 22 and any(r.get("CidrIp") == ip for r in p.get("IpRanges", [])) for p in group["IpPermissions"]):
        aws("ec2", "authorize-security-group-ingress", "--group-id", group["GroupId"],
            "--protocol", "tcp", "--port", "22", "--cidr", ip)
    subnets = aws("ec2", "describe-subnets", "--filters", f"Name=vpc-id,Values={group['VpcId']}",
                  "Name=default-for-az,Values=true")["Subnets"]
    request = {
        "ImageId": args.ami, "InstanceType": "inf2.xlarge", "MinCount": 1, "MaxCount": 1,
        "KeyName": args.key,
        "BlockDeviceMappings": [{"DeviceName": "/dev/sda1", "Ebs": {
            "VolumeSize": args.disk_gib, "VolumeType": "gp3", "Encrypted": True,
            "DeleteOnTermination": True}}],
        "MetadataOptions": {"HttpTokens": "required", "HttpEndpoint": "enabled"},
        "TagSpecifications": [{"ResourceType": kind, "Tags": [
            {"Key": "Name", "Value": NAME}, {"Key": "Project", "Value": "SpeekToMe-Inf2"}]
        } for kind in (["instance", "volume", "spot-instances-request"] if args.market == "spot" else ["instance", "volume"])],
    }
    if args.market == "spot":
        request["InstanceMarketOptions"] = {"MarketType": "spot", "SpotOptions": {
            "SpotInstanceType": "persistent", "InstanceInterruptionBehavior": "stop"}}
    errors = []
    for subnet in [s["SubnetId"] for s in subnets] + [None]:
        interface = {"DeviceIndex": 0, "Groups": [group["GroupId"]],
                     "AssociatePublicIpAddress": True}
        if subnet:
            interface["SubnetId"] = subnet
        request["NetworkInterfaces"] = [interface]
        spec = out / "launch.json"
        spec.write_text(json.dumps(request, indent=2))
        try:
            result = aws("ec2", "run-instances", "--cli-input-json", "file://"+str(spec.resolve()))
        except RuntimeError as e:
            errors.append({"subnet": subnet, "error": str(e)})
            if "InsufficientInstanceCapacity" not in str(e):
                raise
            continue
        (out / "instance-launch.json").write_text(json.dumps(result, indent=2))
        instance = result["Instances"][0]
        assert instance.get("InstanceLifecycle") == ("spot" if args.market == "spot" else None)
        assert instance["InstanceType"] == "inf2.xlarge"
        print(json.dumps({k: instance.get(k) for k in ["InstanceId", "State", "SpotInstanceRequestId"]}, indent=2))
        return
    (out / "launch-errors.json").write_text(json.dumps(errors, indent=2))
    raise SystemExit(f"No inf2.xlarge {args.market} capacity available in {REGION}; no alternative instance launched.")


if __name__ == "__main__":
    main()
