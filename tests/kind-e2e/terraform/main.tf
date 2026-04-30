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

  # Static test credentials for LocalStack — never real AWS keys.
  access_key = "test"
  secret_key = "test"

  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true

  # Path-style is required for LocalStack and non-AWS S3-compatible endpoints.
  s3_use_path_style = true

  endpoints {
    s3 = var.localstack_endpoint
  }
}

resource "aws_s3_bucket" "backup" {
  bucket = var.bucket_name

  # Allow Terraform to delete the bucket even when objects are present,
  # which is the expected state after e2e tests populate it.
  force_destroy = true
}
