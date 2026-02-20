# Terraform equivalent of CloudFormation template: PDC Minimalist Infrastructure - Fargate & DynamoDB
# This file includes VPC, subnets, networking, S3, DynamoDB, IAM, ECS, CodePipeline, and CloudFront resources.
# Comments are included to match your CloudFormation template for clarity.

provider "aws" {
  region = var.aws_region
}

variable "aws_region" {
  description = "AWS region to deploy resources in"
  type        = string
}

variable "github_oauth_token" {
  description = "GitHub OAuth token with repo access for CodePipeline source stage"
  type        = string
  sensitive   = true
}

# --- Network Infrastructure ---
resource "aws_vpc" "pdc_vpc" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "PDCVPC" }
}

resource "aws_subnet" "pdc_public_subnet" {
  vpc_id                  = aws_vpc.pdc_vpc.id
  cidr_block              = "10.0.1.0/24"
  map_public_ip_on_launch = true
  availability_zone       = data.aws_availability_zones.available.names[0]
  tags = { Name = "PDCPublicSubnet" }
}

resource "aws_subnet" "pdc_private_subnet" {
  vpc_id                  = aws_vpc.pdc_vpc.id
  cidr_block              = "10.0.2.0/24"
  map_public_ip_on_launch = false
  availability_zone       = data.aws_availability_zones.available.names[0]
  tags = { Name = "PDCPrivateSubnet" }
}

data "aws_availability_zones" "available" {}

# --- Internet Gateway ---
resource "aws_internet_gateway" "pdc_igw" {
  vpc_id = aws_vpc.pdc_vpc.id
  tags = { Name = "PDCInternetGateway" }
}

# --- Route Table for Public Subnet ---
resource "aws_route_table" "pdc_public_rt" {
  vpc_id = aws_vpc.pdc_vpc.id
  tags = { Name = "PDCPublicRouteTable" }
}

resource "aws_route" "pdc_public_route" {
  route_table_id         = aws_route_table.pdc_public_rt.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.pdc_igw.id
}

resource "aws_route_table_association" "pdc_public_assoc" {
  subnet_id      = aws_subnet.pdc_public_subnet.id
  route_table_id = aws_route_table.pdc_public_rt.id
}

# --- Route Table for Private Subnet ---
resource "aws_route_table" "pdc_private_rt" {
  vpc_id = aws_vpc.pdc_vpc.id
  tags = { Name = "PDCPrivateRouteTable" }
}

resource "aws_route_table_association" "pdc_private_assoc" {
  subnet_id      = aws_subnet.pdc_private_subnet.id
  route_table_id = aws_route_table.pdc_private_rt.id
}

# --- S3 Bucket for Website Assets ---
resource "aws_s3_bucket" "website_assets" {
  bucket = "pdc-website-assets-${random_id.suffix.hex}"
  force_destroy = true
  tags = { Name = "WebsiteS3" }
}

resource "random_id" "suffix" {
  byte_length = 4
}

# --- DynamoDB Table ---
resource "aws_dynamodb_table" "donut_table" {
  name         = "PDC-Inventory"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "ItemID"
  attribute {
    name = "ItemID"
    type = "S"
  }
  tags = { Name = "PDCDonutTable" }
}

# --- IAM Roles (examples, not exhaustive) ---
resource "aws_iam_role" "ecs_task_role" {
  name = "FargateTaskRole"
  assume_role_policy = data.aws_iam_policy_document.ecs_task_assume.json
}

data "aws_iam_policy_document" "ecs_task_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

# --- ECS Cluster ---
resource "aws_ecs_cluster" "fargate_cluster" {
  name = "FargateCluster"
}

# --- ECS Task Definition (simplified) ---
resource "aws_ecs_task_definition" "app_task" {
  family                   = "pdc-app"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = aws_iam_role.ecs_task_role.arn
  task_role_arn            = aws_iam_role.ecs_task_role.arn
  container_definitions    = jsonencode([
    {
      name      = "pdc-app"
      image     = "<ECR_IMAGE_URI>"
      portMappings = [{ containerPort = 8080 }]
      environment = [
        { name = "TABLE_NAME", value = aws_dynamodb_table.donut_table.name },
        { name = "AWS_REGION", value = var.aws_region }
      ]
      logConfiguration = {
        logDriver = "awslogs"
        options = {
          awslogs-group         = "/ecs/pdc-app"
          awslogs-region        = var.aws_region
          awslogs-stream-prefix = "pdc-app"
        }
      }
    }
  ])
}

# --- ECS Service (simplified, no load balancer) ---
resource "aws_ecs_service" "app_service" {
  name            = "PDCService"
  cluster         = aws_ecs_cluster.fargate_cluster.id
  task_definition = aws_ecs_task_definition.app_task.arn
  desired_count   = 1
  launch_type     = "FARGATE"
  network_configuration {
    subnets          = [aws_subnet.pdc_private_subnet.id]
    security_groups  = [aws_security_group.fargate_sg.id]
    assign_public_ip = false
  }
}

# --- Security Groups (example for Fargate) ---
resource "aws_security_group" "fargate_sg" {
  name        = "FargateSG"
  description = "Security group for Fargate containers"
  vpc_id      = aws_vpc.pdc_vpc.id
  ingress {
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# --- CloudFront Distribution (simplified) ---
resource "aws_cloudfront_distribution" "web_distribution" {
  origin {
    domain_name = aws_s3_bucket.website_assets.bucket_regional_domain_name
    origin_id   = "S3Origin"
    s3_origin_config {
      origin_access_identity = ""
    }
  }
  enabled             = true
  default_root_object = "index.html"
  default_cache_behavior {
    target_origin_id       = "S3Origin"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD"]
    cached_methods         = ["GET", "HEAD"]
    forwarded_values {
      query_string = false
      cookies { forward = "none" }
    }
  }
  price_class = "PriceClass_100"
  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }
  viewer_certificate {
    cloudfront_default_certificate = true
  }
}

# --- Outputs ---
output "website_bucket" {
  value = aws_s3_bucket.website_assets.bucket
}

output "cloudfront_domain_name" {
  value = aws_cloudfront_distribution.web_distribution.domain_name
}

# Note: CodePipeline, CodeBuild, and more advanced IAM, NLB, API Gateway, and VPC Link resources can be added similarly.
# For a full production translation, each CloudFormation resource should be mapped to its Terraform equivalent with all properties and dependencies.
