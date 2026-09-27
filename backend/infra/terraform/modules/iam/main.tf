# IAM module — OIDC provider for GitHub Actions, ECR push role, and
# the ArgoCD sync role.

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.50"
    }
  }
}

# GitHub OIDC provider — lets workflows assume roles with no
# long-lived access keys.
resource "aws_iam_openid_connect_provider" "github" {
  url             = "https://token.actions.githubusercontent.com"
  client_id_list  = ["sts.amazonaws.com"]
  thumbprint_list = ["6938fd4d98bab03faadb97b34396831e3780aea1"]
}

# Role assumed by GitHub Actions to push container images to ECR.
resource "aws_iam_role" "ecr_push" {
  name = "${var.name}-ecr-push"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = aws_iam_openid_connect_provider.github.arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "token.actions.githubusercontent.com:aud" = "sts.amazonaws.com"
        }
        StringLike = {
          "token.actions.githubusercontent.com:sub" = [for r in var.github_repos : "repo:${r}:*"]
        }
      }
    }]
  })
}

resource "aws_iam_role_policy" "ecr_push" {
  role = aws_iam_role.ecr_push.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "ecr:GetAuthorizationToken",
          "ecr:BatchCheckLayerAvailability",
          "ecr:GetDownloadUrlForLayer",
          "ecr:BatchGetImage",
          "ecr:InitiateLayerUpload",
          "ecr:UploadLayerPart",
          "ecr:CompleteLayerUpload",
          "ecr:PutImage",
        ]
        Resource = "*"
      },
    ]
  })
}

# Role assumed by ArgoCD to read ECR + sync application manifests.
# The ArgoCD pod gets this via IRSA (IAM Roles for Service Accounts);
# OIDC provider for the EKS cluster is in the eks module.
resource "aws_iam_role" "argocd" {
  count = var.eks_oidc_provider_arn != "" ? 1 : 0

  name = "${var.name}-argocd"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Federated = var.eks_oidc_provider_arn }
      Action    = "sts:AssumeRoleWithWebIdentity"
      Condition = {
        StringEquals = {
          "${trimprefix(var.eks_oidc_provider_url, "https://")}:sub" = "system:serviceaccount:argocd:argocd-application-controller"
        }
      }
    }]
  })
}
