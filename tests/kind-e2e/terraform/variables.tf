variable "bucket_name" {
  description = "S3 bucket name for dumpscript e2e backups."
  type        = string
  default     = "dumpscript-e2e"
}

variable "localstack_endpoint" {
  description = "LocalStack S3 endpoint URL (port-forwarded to the test host)."
  type        = string
  default     = "http://localhost:4566"
}
