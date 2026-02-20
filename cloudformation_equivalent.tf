terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

# --- Network Infrastructure ---
resource "aws_vpc" "pdc_vpc" {
  cidr_block           = "10.0.0.0/16"
  # below 2 provides internal DNS server. Allows addressing like: aws.s3.bucket_id or aws.dynamo.table_name
  enable_dns_support   = true # allows translation from hostname to ip. Addr 169.254.169.253 (link-local, hypervisor level) and bottom ip in CIDR block + 2 (10.0.0.2, network level) point to the internal DNS server
  enable_dns_hostnames = true # auto gives everything a hostname
  tags = { Name = "PDC-VPC" }
}

resource "aws_subnet" "pdc_public_subnet" {
  vpc_id                  = aws_vpc.pdc_vpc.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true # auto give resources launced in this subneta public IP 
}

# --- Database: DynamoDB (On-Demand) ---
resource "aws_dynamodb_table" "pdc_inventory" {
  name         = "PDC-Inventory"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "StoreID"
  range_key    = "ItemID"

  attribute {
    name = "StoreID"
    type = "S"
  }

  attribute {
    name = "ItemID"
    type = "S"
  }
}

# --- IAM Roles for Fargate ---
resource "aws_iam_role" "fargate_task_role" {
  name = "PDCFargateTaskRole"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action = "sts:AssumeRole"
      Effect = "Allow"
      Principal = { Service = "ecs-tasks.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "dynamo_access" {
  name = "DynamoDBReadAccess"
  role = aws_iam_role.fargate_task_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action   = ["dynamodb:GetItem", "dynamodb:Query", "dynamodb:Scan"]
      Effect   = "Allow"
      Resource = aws_dynamodb_table.pdc_inventory.arn
    }]
  })
}

# --- Fargate Cluster & Service ---
resource "aws_ecs_cluster" "pdc_cluster" {
  name = "PDC-Donut-Cluster"
}

# # if using EC2 or external launch type this is how to attach compute resources (fargate does not require this step)

# resource "aws_launch_template" "pdc_ec2_template" {
#   name_id_prefix = "pdc-ec2-template"
#   image_id      = "ami-0c55b159cbfafe1f0" # Must be an ECS-Optimized AMI
#   instance_type = "t3.micro"

#   # THIS IS THE ATTACHMENT PIECE
#   user_data = base64encode(<<-EOF
#               #!/bin/bash
#               echo "ECS_CLUSTER=${aws_ecs_cluster.pdc_cluster.name}" >> /etc/ecs/ecs.config
#               EOF
#   )

#   iam_instance_profile {
#     name = aws_iam_instance_profile.ecs_node_profile.name
#   }
# }

# notes: this is an EC2 autoscaling group. this scales your compute power. you also need an application auto scaling group which will scale up your containers.

# # 2. The Auto Scaling Group (The "Fleet")
# resource "aws_autoscaling_group" "pdc_asg" {
#   vpc_zone_identifier = [aws_subnet.pdc_public_subnet.id]
#   desired_capacity    = 2
#   max_size            = 5
#   min_size            = 1

#   launch_template {
#     id      = aws_launch_template.pdc_ec2_template.id
#     version = "$Latest"
#   }
# }

resource "aws_ecs_task_definition" "pdc_task" { # recipe with which to create containers
  family                   = "pdc-app-task"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  task_role_arn            = aws_iam_role.fargate_task_role.arn

  container_definitions = jsonencode([{
    name  = "pdc-app"
    image = "nginx:latest" # Replace with your ECR image URI
    portMappings = [{
      containerPort = 80
      # hostPort    = 0 # for EC2 launch type with dynamic port mapping.
    }]
    environment = [{
      name  = "TABLE_NAME"
      value = aws_dynamodb_table.pdc_inventory.name
    }]
    health_check = {
      command     = ["CMD-SHELL", "curl -f http://localhost/ || exit 1"]
      interval    = 30
      timeout     = 5
      retries     = 3
      startPeriod = 60
    }
  }])
}

resource "aws_ecs_service" "pdc_service" {  # manages tasks (containers), is the control node
  name            = "pdc-service"
  cluster         = aws_ecs_cluster.pdc_cluster.id
  task_definition = aws_ecs_task_definition.pdc_task.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.pdc_public_subnet.id]
    assign_public_ip = true
  }

  # for EC2 launch type
  # load_balancer {
  #   target_group_arn = aws_lb_target_group.pdc_tg.arn
  #   container_name   = "donut-app" # Must match the name in Task Definition
  #   container_port   = 80         # The port INSIDE the container
  # }
}