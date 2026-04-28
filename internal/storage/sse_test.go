package storage

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/cloudscript-technology/dumpscript/internal/config"
)

func TestApplySSE_NoOpWhenEmpty(t *testing.T) {
	in := &s3.PutObjectInput{}
	applySSE(in, config.S3{})
	if in.ServerSideEncryption != "" {
		t.Errorf("ServerSideEncryption = %q, want empty", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId != nil {
		t.Errorf("SSEKMSKeyId = %v, want nil", in.SSEKMSKeyId)
	}
}

func TestApplySSE_AES256(t *testing.T) {
	in := &s3.PutObjectInput{}
	applySSE(in, config.S3{SSE: "AES256"})
	if in.ServerSideEncryption != s3types.ServerSideEncryptionAes256 {
		t.Errorf("ServerSideEncryption = %q, want AES256", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId != nil {
		t.Errorf("SSEKMSKeyId = %v, want nil for AES256", in.SSEKMSKeyId)
	}
}

func TestApplySSE_KMSWithKeyID(t *testing.T) {
	in := &s3.PutObjectInput{}
	applySSE(in, config.S3{
		SSE:         "aws:kms",
		SSEKMSKeyID: "arn:aws:kms:us-east-1:123:key/abc",
	})
	if in.ServerSideEncryption != s3types.ServerSideEncryptionAwsKms {
		t.Errorf("ServerSideEncryption = %q, want aws:kms", in.ServerSideEncryption)
	}
	if aws.ToString(in.SSEKMSKeyId) != "arn:aws:kms:us-east-1:123:key/abc" {
		t.Errorf("SSEKMSKeyId = %q", aws.ToString(in.SSEKMSKeyId))
	}
}

func TestApplySSE_KMSWithoutKeyIDUsesBucketDefault(t *testing.T) {
	in := &s3.PutObjectInput{}
	applySSE(in, config.S3{SSE: "aws:kms"})
	if in.ServerSideEncryption != s3types.ServerSideEncryptionAwsKms {
		t.Errorf("ServerSideEncryption = %q, want aws:kms", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId != nil {
		t.Errorf("SSEKMSKeyId = %v, want nil to fall back to bucket default", in.SSEKMSKeyId)
	}
}
