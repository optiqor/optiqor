# Staging environment: us-east-2 multi-AZ, scaled-down prod twin so
# the auto-deploy on every tagged commit exercises the same code path.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
  backend "s3" {
    bucket         = "sevro-tfstate-staging"
    key            = "envs/staging/terraform.tfstate"
    region         = "us-east-2"
    dynamodb_table = "sevro-tfstate-staging-lock"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Project     = "sevro"
      Environment = "staging"
      Tenant      = "shared"
      ManagedBy   = "terraform"
    }
  }
}

module "kms" {
  source       = "../../modules/kms"
  name         = "sevro-staging"
  multi_region = false
}

module "vpc" {
  source = "../../modules/vpc"
  name   = "sevro-staging"
  cidr   = "10.20.0.0/16"
  azs    = ["us-east-2a", "us-east-2b", "us-east-2c"]
}

module "eks" {
  source             = "../../modules/eks"
  name               = "sevro-staging"
  kubernetes_version = "1.31"
  subnet_ids         = module.vpc.private_subnet_ids
  kms_key_arn        = module.kms.data_key_arn
  public_endpoint    = false
}

module "rds" {
  source                   = "../../modules/rds-postgres"
  name                     = "sevro-staging"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.db_subnet_ids
  allow_security_group_ids = []
  kms_key_arn              = module.kms.data_key_arn
  instance_class           = "db.r6g.large"
  allocated_storage_gb     = 100
  multi_az                 = true
  deletion_protection      = false
}

module "redis" {
  source                   = "../../modules/elasticache"
  name                     = "sevro-staging"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.private_subnet_ids
  allow_security_group_ids = []
  kms_key_arn              = module.kms.data_key_arn
  num_cache_clusters       = 2
}

module "s3_receipts" {
  source                       = "../../modules/s3"
  name                         = "sevro-staging-receipts"
  kms_key_arn                  = module.kms.data_key_arn
  expire_noncurrent_after_days = 30
}

module "s3_sandbox" {
  source                       = "../../modules/s3"
  name                         = "sevro-staging-sandbox"
  kms_key_arn                  = module.kms.data_key_arn
  expire_noncurrent_after_days = 7
}

module "iam" {
  source                = "../../modules/iam"
  name                  = "sevro-staging"
  github_repos          = ["lowplane/backend"]
  eks_oidc_provider_arn = module.eks.oidc_provider_arn
  eks_oidc_provider_url = module.eks.oidc_provider_url
}
