# OpenLoadBalancer Terraform Module
# Supports AWS, GCP, Azure

terraform {
  required_version = ">= 1.0"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.0"
    }
  }
}

# AWS Security Group
resource "aws_security_group" "olb" {
  count       = var.cloud_provider == "aws" ? 1 : 0
  name_prefix = "${var.name}-"
  description = "Security group for OpenLoadBalancer"
  vpc_id      = var.vpc_id

  # HTTP
  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = var.allowed_cidr
  }

  # HTTPS
  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = var.allowed_cidr
  }

  # Admin API
  ingress {
    from_port   = 8081
    to_port     = 8081
    protocol    = "tcp"
    cidr_blocks = var.admin_cidr
  }

  # MCP Server
  dynamic "ingress" {
    for_each = var.mcp_enabled ? [1] : []
    content {
      from_port   = 8082
      to_port     = 8082
      protocol    = "tcp"
      cidr_blocks = var.admin_cidr
    }
  }

  # Health check port
  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = [var.vpc_cidr]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.tags, {
    Name = var.name
  })

  lifecycle {
    create_before_destroy = true
  }
}

# IAM Role for OLB instances
resource "aws_iam_role" "olb" {
  count = var.cloud_provider == "aws" && var.create_iam_role ? 1 : 0

  name = "${var.name}-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = {
        Service = "ec2.amazonaws.com"
      }
    }]
  })

  tags = var.tags
}

resource "aws_iam_role_policy" "olb" {
  count = var.cloud_provider == "aws" && var.create_iam_role ? 1 : 0

  name = "${var.name}-policy"
  role = aws_iam_role.olb[0].id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ec2:DescribeInstances",
          "ec2:DescribeTags",
          "cloudwatch:PutMetricData"
        ]
        Resource = "*"
      },
      {
        Effect = "Allow"
        Action = [
          "s3:GetObject",
          "s3:ListBucket"
        ]
        Resource = [
          "arn:aws:s3:::${var.config_bucket}",
          "arn:aws:s3:::${var.config_bucket}/*"
        ]
      }
    ]
  })
}

resource "aws_iam_instance_profile" "olb" {
  count = var.cloud_provider == "aws" && var.create_iam_role ? 1 : 0

  name = "${var.name}-profile"
  role = aws_iam_role.olb[0].name

  tags = var.tags
}

# Launch Template
resource "aws_launch_template" "olb" {
  count = var.cloud_provider == "aws" ? 1 : 0

  name_prefix   = "${var.name}-"
  image_id      = var.ami_id
  instance_type = var.instance_type

  iam_instance_profile {
    name = var.create_iam_role ? aws_iam_instance_profile.olb[0].name : var.iam_instance_profile
  }

  vpc_security_group_ids = [aws_security_group.olb[0].id]

  user_data = base64encode(templatefile("${path.module}/templates/userdata.sh", {
    config_bucket = var.config_bucket
    cluster_name  = var.name
  }))

  tag_specifications {
    resource_type = "instance"
    tags = merge(var.tags, {
      Name = var.name
    })
  }

  lifecycle {
    create_before_destroy = true
  }
}

# Auto Scaling Group
resource "aws_autoscaling_group" "olb" {
  count = var.cloud_provider == "aws" ? 1 : 0

  name                = var.name
  vpc_zone_identifier = var.subnet_ids
  target_group_arns   = aws_lb_target_group.olb[*].arn
  health_check_type   = "ELB"
  min_size            = var.min_size
  max_size            = var.max_size
  desired_capacity    = var.desired_capacity

  launch_template {
    id      = aws_launch_template.olb[0].id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = var.name
    propagate_at_launch = true
  }

  dynamic "tag" {
    for_each = var.tags
    content {
      key                 = tag.key
      value               = tag.value
      propagate_at_launch = true
    }
  }
}

# Application Load Balancer
resource "aws_lb" "olb" {
  count = var.cloud_provider == "aws" && var.create_load_balancer ? 1 : 0

  name               = var.name
  internal           = var.internal
  load_balancer_type = "application"
  security_groups    = [aws_security_group.olb[0].id]
  subnets            = var.subnet_ids

  enable_deletion_protection = var.enable_deletion_protection
  enable_http2               = true

  tags = var.tags
}

resource "aws_lb_target_group" "olb" {
  count = var.cloud_provider == "aws" && var.create_load_balancer ? 1 : 0

  name     = var.name
  port     = 80
  protocol = "HTTP"
  vpc_id   = var.vpc_id

  health_check {
    enabled             = true
    healthy_threshold   = 2
    interval            = 10
    matcher             = "200"
    path                = "/health"
    port                = "traffic-port"
    protocol            = "HTTP"
    timeout             = 5
    unhealthy_threshold = 3
  }

  tags = var.tags
}

resource "aws_lb_listener" "http" {
  count = var.cloud_provider == "aws" && var.create_load_balancer ? 1 : 0

  load_balancer_arn = aws_lb.olb[0].arn
  port              = "80"
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.olb[0].arn
  }
}

resource "aws_lb_listener" "https" {
  count = var.cloud_provider == "aws" && var.create_load_balancer && var.certificate_arn != "" ? 1 : 0

  load_balancer_arn = aws_lb.olb[0].arn
  port              = "443"
  protocol          = "HTTPS"
  ssl_policy        = "ELBSecurityPolicy-TLS13-1-2-2021-06"
  certificate_arn   = var.certificate_arn

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.olb[0].arn
  }
}

# CloudWatch Dashboard
resource "aws_cloudwatch_dashboard" "olb" {
  count = var.cloud_provider == "aws" && var.create_monitoring ? 1 : 0

  dashboard_name = "${var.name}-dashboard"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          metrics = [["AWS/EC2", "CPUUtilization", "AutoScalingGroupName", var.name]]
          period  = 300
          stat    = "Average"
          region  = var.aws_region
          title   = "CPU Utilization"
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          metrics = [["AWS/ApplicationELB", "RequestCount", "LoadBalancer", aws_lb.olb[0].arn_suffix]]
          period  = 300
          stat    = "Sum"
          region  = var.aws_region
          title   = "Request Count"
        }
      }
    ]
  })
}
