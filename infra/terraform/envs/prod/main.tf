# Production environment: us-east-1 multi-AZ, DR-baseline configured.
# State lives in a dedicated S3 bucket with DynamoDB locking; the
# bucket and table are bootstrapped manually before the first apply.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
  backend "s3" {
    bucket         = "optiqor-tfstate-prod"
    key            = "prod/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "optiqor-tfstate-prod-lock"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region
  default_tags {
    tags = {
      Project     = "optiqor"
      Environment = "prod"
      Tenant      = "shared"
      ManagedBy   = "terraform"
    }
  }
}

# Cross-region provider for multi-region KMS replicas and S3 CRR
# destination buckets.
provider "aws" {
  alias  = "dr"
  region = "us-east-2"
  default_tags {
    tags = {
      Project     = "optiqor"
      Environment = "prod"
      Tenant      = "shared"
      ManagedBy   = "terraform"
      Role        = "dr-replica"
    }
  }
}

module "kms" {
  source       = "../../modules/kms"
  name         = "optiqor-prod"
  multi_region = true
}

module "vpc" {
  source = "../../modules/vpc"
  name   = "optiqor-prod"
  cidr   = "10.0.0.0/16"
  azs    = ["us-east-1a", "us-east-1b", "us-east-1c"]
}

module "eks" {
  source             = "../../modules/eks"
  name               = "optiqor-prod"
  kubernetes_version = "1.31"
  subnet_ids         = module.vpc.private_subnet_ids
  kms_key_arn        = module.kms.data_key_arn
  public_endpoint    = false
}

module "rds" {
  source                   = "../../modules/rds-postgres"
  name                     = "optiqor-prod"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.db_subnet_ids
  allow_security_group_ids = []
  kms_key_arn              = module.kms.data_key_arn
  instance_class           = "db.r6g.xlarge"
  allocated_storage_gb     = 200
  multi_az                 = true
  deletion_protection      = true
}

module "redis" {
  source                   = "../../modules/elasticache"
  name                     = "optiqor-prod"
  vpc_id                   = module.vpc.vpc_id
  subnet_ids               = module.vpc.private_subnet_ids
  allow_security_group_ids = []
  kms_key_arn              = module.kms.data_key_arn
  num_cache_clusters       = 2
}

# Receipts bucket — never-expire (7y retention enforced server-side
# by the gdpr package) + CRR to us-east-2 for DR.
module "s3_receipts" {
  source                             = "../../modules/s3"
  name                               = "optiqor-prod-receipts"
  kms_key_arn                        = module.kms.data_key_arn
  expire_noncurrent_after_days       = 0
  replication_destination_bucket_arn = "arn:aws:s3:::optiqor-prod-receipts-dr"
}

module "s3_sandbox" {
  source                             = "../../modules/s3"
  name                               = "optiqor-prod-sandbox"
  kms_key_arn                        = module.kms.data_key_arn
  expire_noncurrent_after_days       = 30
  replication_destination_bucket_arn = "arn:aws:s3:::optiqor-prod-sandbox-dr"
}

module "s3_cur" {
  source                       = "../../modules/s3"
  name                         = "optiqor-prod-cur"
  kms_key_arn                  = module.kms.data_key_arn
  expire_noncurrent_after_days = 365
}

module "iam" {
  source                = "../../modules/iam"
  name                  = "optiqor-prod"
  github_repos          = ["optiqor/backend"]
  eks_oidc_provider_arn = module.eks.oidc_provider_arn
  eks_oidc_provider_url = module.eks.oidc_provider_url
}
