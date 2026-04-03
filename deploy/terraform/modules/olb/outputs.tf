output "security_group_id" {
  description = "Security group ID"
  value       = var.cloud_provider == "aws" ? aws_security_group.olb[0].id : null
}

output "autoscaling_group_name" {
  description = "Auto Scaling Group name"
  value       = var.cloud_provider == "aws" ? aws_autoscaling_group.olb[0].name : null
}

output "load_balancer_dns" {
  description = "Load balancer DNS name"
  value       = var.cloud_provider == "aws" && var.create_load_balancer ? aws_lb.olb[0].dns_name : null
}

output "load_balancer_arn" {
  description = "Load balancer ARN"
  value       = var.cloud_provider == "aws" && var.create_load_balancer ? aws_lb.olb[0].arn : null
}

output "target_group_arn" {
  description = "Target group ARN"
  value       = var.cloud_provider == "aws" && var.create_load_balancer ? aws_lb_target_group.olb[0].arn : null
}

output "iam_role_arn" {
  description = "IAM role ARN"
  value       = var.cloud_provider == "aws" && var.create_iam_role ? aws_iam_role.olb[0].arn : null
}
