# OpenLoadBalancer Terraform Module

Terraform module for deploying OpenLoadBalancer on AWS.

## Usage

```hcl
module "olb" {
  source = "github.com/openloadbalancer/olb//deploy/terraform/modules/olb"

  name           = "my-olb"
  cloud_provider = "aws"
  vpc_id         = "vpc-12345678"
  vpc_cidr       = "10.0.0.0/16"
  subnet_ids     = ["subnet-1", "subnet-2"]
  ami_id         = "ami-12345678"
  
  config_bucket = "my-olb-config"
  
  min_size         = 2
  max_size         = 10
  desired_capacity = 3
  
  certificate_arn = "arn:aws:acm:us-east-1:123456789012:certificate/..."
  
  tags = {
    Environment = "production"
  }
}
```

## Requirements

| Name | Version |
|------|---------|
| terraform | >= 1.0 |
| aws | >= 5.0 |

## Inputs

See `variables.tf` for complete list.

## Outputs

| Name | Description |
|------|-------------|
| load_balancer_dns | ALB DNS name |
| autoscaling_group_name | ASG name |
| security_group_id | Security group ID |
| target_group_arn | Target group ARN |

## License

Apache 2.0
