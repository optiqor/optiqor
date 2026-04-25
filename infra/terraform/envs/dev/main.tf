terraform {
  required_version = ">= 1.7.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.60"
    }
  }

  # Remote state — uncomment + fill once the S3 + DynamoDB resources exist.
  # backend "s3" {
  #   bucket         = "costify-terraform-state-dev"
  #   key            = "envs/dev/terraform.tfstate"
  #   region         = "us-east-2"
  #   dynamodb_table = "costify-terraform-locks-dev"
  #   encrypt        = true
  # }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "costify"
      Environment = "dev"
      ManagedBy   = "terraform"
    }
  }
}

# Modules wire up in Phase 1 (Weeks 1-2). See ../../modules/ for skeletons.
# Intentionally empty today so `terraform validate` passes against scaffolding.
