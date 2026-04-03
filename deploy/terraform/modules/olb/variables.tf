variable "name" {
  description = "Name prefix for all resources"
  type        = string
}

variable "cloud_provider" {
  description = "Cloud provider (aws, gcp, azure)"
  type        = string
  default     = "aws"
}

variable "vpc_id" {
  description = "VPC ID"
  type        = string
}

variable "vpc_cidr" {
  description = "VPC CIDR block"
  type        = string
}

variable "subnet_ids" {
  description = "List of subnet IDs"
  type        = list(string)
}

variable "ami_id" {
  description = "AMI ID for OLB instances"
  type        = string
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

variable "min_size" {
  description = "Minimum number of instances"
  type        = number
  default     = 2
}

variable "max_size" {
  description = "Maximum number of instances"
  type        = number
  default     = 10
}

variable "desired_capacity" {
  description = "Desired number of instances"
  type        = number
  default     = 2
}

variable "allowed_cidr" {
  description = "Allowed CIDR blocks for HTTP/HTTPS"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "admin_cidr" {
  description = "Allowed CIDR blocks for admin API"
  type        = list(string)
  default     = []
}

variable "config_bucket" {
  description = "S3 bucket for OLB configuration"
  type        = string
}

variable "create_iam_role" {
  description = "Create IAM role for OLB instances"
  type        = bool
  default     = true
}

variable "iam_instance_profile" {
  description = "IAM instance profile name (if not creating)"
  type        = string
  default     = ""
}

variable "create_load_balancer" {
  description = "Create Application Load Balancer"
  type        = bool
  default     = true
}

variable "internal" {
  description = "Create internal load balancer"
  type        = bool
  default     = false
}

variable "certificate_arn" {
  description = "ACM certificate ARN for HTTPS"
  type        = string
  default     = ""
}

variable "enable_deletion_protection" {
  description = "Enable deletion protection for ALB"
  type        = bool
  default     = false
}

variable "mcp_enabled" {
  description = "Enable MCP server"
  type        = bool
  default     = false
}

variable "create_monitoring" {
  description = "Create CloudWatch dashboard"
  type        = bool
  default     = true
}

variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "tags" {
  description = "Tags to apply to resources"
  type        = map(string)
  default     = {}
}
