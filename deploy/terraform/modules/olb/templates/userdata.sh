#!/bin/bash
set -e

# OpenLoadBalancer User Data Script
# Configures an EC2 instance as an OLB node

export CONFIG_BUCKET="${config_bucket}"
export CLUSTER_NAME="${cluster_name}"

# Install dependencies
yum update -y
yum install -y aws-cli docker

# Start Docker
systemctl enable docker
systemctl start docker

# Create OLB directories
mkdir -p /etc/olb/certs /etc/olb/configs /var/log/olb

# Download configuration from S3
aws s3 cp s3://$CONFIG_BUCKET/olb.yaml /etc/olb/configs/olb.yaml || echo "Using default config"

# Pull and run OLB container
docker run -d \
  --name olb \
  --restart always \
  --network host \
  -v /etc/olb:/etc/olb:ro \
  -v /var/log/olb:/var/log/olb \
  -e CLUSTER_NAME=$CLUSTER_NAME \
  ghcr.io/openloadbalancer/olb:latest \
  start --config /etc/olb/configs/olb.yaml

# CloudWatch agent for metrics
yum install -y amazon-cloudwatch-agent

cat > /opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json <<'CWCONFIG'
{
  "metrics": {
    "namespace": "OpenLoadBalancer",
    "metrics_collected": {
      "disk": {
        "measurement": ["used_percent"],
        "resources": ["*"]
      },
      "mem": {
        "measurement": ["mem_used_percent"]
      },
      "cpu": {
        "measurement": ["cpu_usage_idle", "cpu_usage_iowait", "cpu_usage_user", "cpu_usage_system"],
        "metrics_collection_interval": 60,
        "totalcpu": true
      }
    }
  },
  "logs": {
    "logs_collected": {
      "files": {
        "collect_list": [
          {
            "file_path": "/var/log/olb/*.log",
            "log_group_name": "/openloadbalancer/olb",
            "log_stream_name": "{instance_id}"
          }
        ]
      }
    }
  }
}
CWCONFIG

/opt/aws/amazon-cloudwatch-agent/bin/amazon-cloudwatch-agent-ctl -a fetch-config -m ec2 -s -c file:/opt/aws/amazon-cloudwatch-agent/etc/amazon-cloudwatch-agent.json
