variable "name" {
  description = "Name prefix"
  type        = string
  default     = "olb"
}

variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "environment" {
  description = "Environment name"
  type        = string
  default     = "production"
}

variable "instance_type" {
  description = "EC2 instance type"
  type        = string
  default     = "t3.medium"
}

variable "min_size" {
  description = "Minimum instances"
  type        = number
  default     = 2
}

variable "max_size" {
  description = "Maximum instances"
  type        = number
  default     = 6
}

variable "desired_capacity" {
  description = "Desired instances"
  type        = number
  default     = 2
}

variable "allowed_cidr" {
  description = "Allowed CIDR for HTTP/HTTPS"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "admin_cidr" {
  description = "Allowed CIDR for admin API"
  type        = list(string)
  default     = []
}

variable "certificate_arn" {
  description = "ACM certificate ARN"
  type        = string
  default     = ""
}

variable "backend_addresses" {
  description = "Backend server addresses"
  type        = list(string)
  default     = ["10.0.1.10:8080", "10.0.1.11:8080"]
}
