provider "aws" {
  region = var.region
}

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# Get latest Amazon Linux 2 AMI
data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["amzn2-ami-hvm-*-x86_64-gp2"]
  }
}

# S3 bucket for configuration
resource "aws_s3_bucket" "config" {
  bucket = "${var.name}-config-${random_string.suffix.result}"
}

resource "random_string" "suffix" {
  length  = 8
  special = false
  upper   = false
}

resource "aws_s3_bucket_object" "config" {
  bucket = aws_s3_bucket.config.bucket
  key    = "olb.yaml"
  content = templatefile("${path.module}/olb.yaml.tpl", {
    backends = var.backend_addresses
  })
}

# OLB Module
module "olb" {
  source = "../../modules/olb"

  name             = var.name
  cloud_provider   = "aws"
  vpc_id           = data.aws_vpc.default.id
  vpc_cidr         = data.aws_vpc.default.cidr_block
  subnet_ids       = data.aws_subnets.default.ids
  ami_id           = data.aws_ami.amazon_linux.id
  instance_type    = var.instance_type
  config_bucket    = aws_s3_bucket.config.bucket
  min_size         = var.min_size
  max_size         = var.max_size
  desired_capacity = var.desired_capacity
  allowed_cidr     = var.allowed_cidr
  admin_cidr       = var.admin_cidr
  certificate_arn  = var.certificate_arn
  aws_region       = var.region

  tags = {
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}
