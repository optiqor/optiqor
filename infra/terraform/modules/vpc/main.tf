# VPC module — three-tier subnet layout (public / private / db) across three AZs.
# Flow logs land in CloudWatch with 90-day retention.
#
# Caller binds default_tags so every resource carries Project/Environment/Tenant.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

resource "aws_vpc" "this" {
  cidr_block           = var.cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = {
    Name = "${var.name}-vpc"
  }
}

resource "aws_subnet" "public" {
  count                   = length(var.azs)
  vpc_id                  = aws_vpc.this.id
  cidr_block              = cidrsubnet(var.cidr, 4, count.index)
  availability_zone       = var.azs[count.index]
  map_public_ip_on_launch = false

  tags = {
    Name                     = "${var.name}-public-${var.azs[count.index]}"
    Tier                     = "public"
    "kubernetes.io/role/elb" = "1"
  }
}

resource "aws_subnet" "private" {
  count             = length(var.azs)
  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet(var.cidr, 4, count.index + length(var.azs))
  availability_zone = var.azs[count.index]

  tags = {
    Name                              = "${var.name}-private-${var.azs[count.index]}"
    Tier                              = "private"
    "kubernetes.io/role/internal-elb" = "1"
  }
}

resource "aws_subnet" "db" {
  count             = length(var.azs)
  vpc_id            = aws_vpc.this.id
  cidr_block        = cidrsubnet(var.cidr, 4, count.index + 2 * length(var.azs))
  availability_zone = var.azs[count.index]

  tags = {
    Name = "${var.name}-db-${var.azs[count.index]}"
    Tier = "db"
  }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = "${var.name}-igw" }
}

resource "aws_eip" "nat" {
  count  = length(var.azs)
  domain = "vpc"
  tags   = { Name = "${var.name}-nat-${var.azs[count.index]}" }
}

resource "aws_nat_gateway" "this" {
  count         = length(var.azs)
  allocation_id = aws_eip.nat[count.index].id
  subnet_id     = aws_subnet.public[count.index].id
  tags          = { Name = "${var.name}-nat-${var.azs[count.index]}" }
}

resource "aws_flow_log" "this" {
  log_destination      = aws_cloudwatch_log_group.flow.arn
  log_destination_type = "cloud-watch-logs"
  iam_role_arn         = aws_iam_role.flow.arn
  traffic_type         = "ALL"
  vpc_id               = aws_vpc.this.id
}

resource "aws_cloudwatch_log_group" "flow" {
  name              = "/optiqor/vpc/${var.name}/flow"
  retention_in_days = 90
}

resource "aws_iam_role" "flow" {
  name = "${var.name}-vpc-flow-logs"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "vpc-flow-logs.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "flow" {
  role       = aws_iam_role.flow.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonAPIGatewayPushToCloudWatchLogs"
}
