# Dev environment: us-east-2 single-AZ. Smallest viable stack so
# engineers can iterate without burning prod-tier money.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
  # Remote state — uncomment + fill once the S3 + DynamoDB resources exist.
  # backend "s3" {
  #   bucket         = "optiqor-tfstate-dev"
  #   key            = "envs/dev/terraform.tfstate"
  #   region         = "us-east-2"
  #   dynamodb_table = "optiqor-tfstate-dev-lock"
  #   encrypt        = true
  # }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Project     = "optiqor"
      Environment = "dev"
      Tenant      = "shared"
      ManagedBy   = "terraform"
    }
  }
}

module "kms" {
  source       = "../../modules/kms"
  name         = "optiqor-dev"
  multi_region = false
}

module "vpc" {
  source = "../../modules/vpc"
  name   = "optiqor-dev"
  cidr   = "10.10.0.0/16"
  azs    = ["us-east-2a", "us-east-2b"]
}

module "eks" {
  source             = "../../modules/eks"
  name               = "optiqor-dev"
  kubernetes_version = "1.31"
  subnet_ids         = module.vpc.private_subnet_ids
  kms_key_arn        = module.kms.data_key_arn
  public_endpoint    = true
}

module "rds" {
  source                   = "../../modules/rds-postgres"
  name                     = "optiqor-dev"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.db_subnet_ids
  allow_security_group_ids = [module.eks.cluster_security_group_id]
  kms_key_arn              = module.kms.data_key_arn
  instance_class           = "db.t4g.medium"
  allocated_storage_gb     = 50
  multi_az                 = false
  deletion_protection      = false
}

module "redis" {
  source                   = "../../modules/elasticache"
  name                     = "optiqor-dev"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.private_subnet_ids
  allow_security_group_ids = [module.eks.cluster_security_group_id]
  kms_key_arn              = module.kms.data_key_arn
  num_cache_clusters       = 1
}

module "s3_receipts" {
  source                       = "../../modules/s3"
  name                         = "optiqor-dev-receipts"
  kms_key_arn                  = module.kms.data_key_arn
  expire_noncurrent_after_days = 30
}

module "s3_sandbox" {
  source                       = "../../modules/s3"
  name                         = "optiqor-dev-sandbox"
  kms_key_arn                  = module.kms.data_key_arn
  expire_noncurrent_after_days = 7
}

module "iam" {
  source                = "../../modules/iam"
  name                  = "optiqor-dev"
  github_repos          = ["optiqor/backend"]
  eks_oidc_provider_arn = module.eks.oidc_provider_arn
  eks_oidc_provider_url = module.eks.oidc_provider_url
}
