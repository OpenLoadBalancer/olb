output "load_balancer_dns" {
  description = "Load balancer DNS"
  value       = module.olb.load_balancer_dns
}

output "autoscaling_group_name" {
  description = "Auto Scaling Group name"
  value       = module.olb.autoscaling_group_name
}

output "security_group_id" {
  description = "Security group ID"
  value       = module.olb.security_group_id
}

output "config_bucket" {
  description = "Configuration S3 bucket"
  value       = aws_s3_bucket.config.bucket
}
