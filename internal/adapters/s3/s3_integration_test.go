//go:build integration

package s3

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/josephjohncox/effectus/internal/adapters"
)

func minioClient(t *testing.T) (*awss3.Client, string) {
	t.Helper()
	endpoint, region, bucket := os.Getenv("S3_ENDPOINT"), os.Getenv("S3_REGION"), os.Getenv("S3_BUCKET")
	key, secret := os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
	if endpoint == "" || bucket == "" {
		t.Skip("S3_ENDPOINT and S3_BUCKET not set")
	}
	if region == "" {
		region = "us-east-1"
	}
	cfg, err := awsconfig.LoadDefaultConfig(t.Context(), awsconfig.WithRegion(region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(key, secret, "")))
	if err != nil {
		t.Fatal(err)
	}
	client := awss3.NewFromConfig(cfg, func(options *awss3.Options) {
		options.UsePathStyle = true
		options.BaseEndpoint = aws.String(endpoint)
	})
	if _, err := client.HeadBucket(t.Context(), &awss3.HeadBucketInput{Bucket: aws.String(bucket)}); err != nil {
		if _, createErr := client.CreateBucket(t.Context(), &awss3.CreateBucketInput{Bucket: aws.String(bucket)}); createErr != nil {
			t.Fatalf("MinIO bucket unavailable: %v (create: %v)", err, createErr)
		}
	}
	return client, bucket
}

func putObject(t *testing.T, client *awss3.Client, bucket, key, body string) {
	t.Helper()
	_, err := client.PutObject(t.Context(), &awss3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: strings.NewReader(body)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestS3StreamMaxObjectsAndBackpressureIntegration(t *testing.T) {
	client, bucket := minioClient(t)
	prefix := fmt.Sprintf("lossless/%d/", time.Now().UnixNano())
	for _, name := range []string{"a.json", "b.json", "c.json"} {
		putObject(t, client, bucket, prefix+name, fmt.Sprintf(`{"key":%q}`, name))
	}
	source, err := NewSource(&Config{
		SourceID: "integration", Region: "us-east-1", Bucket: bucket, Prefix: prefix,
		Mode: "stream", Format: "json", MaxObjects: 2, MaxObjectBytes: 1 << 20,
		Endpoint: os.Getenv("S3_ENDPOINT"), ForcePathStyle: true,
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"), Timeout: 10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer source.Stop(context.Background())
	out := make(chan *adapters.TypedFact, 3)
	if err := pollAndAcknowledge(t, source, out, 2); err != nil {
		t.Fatal(err)
	}
	if err := pollAndAcknowledge(t, source, out, 1); err != nil {
		t.Fatal(err)
	}

	var ndjson bytes.Buffer
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&ndjson, "{\"n\":%d}\n", i)
	}
	multiKey := prefix + "z.ndjson"
	putObject(t, client, bucket, multiKey, ndjson.String())
	multi, err := NewSource(&Config{
		SourceID: "integration-multi", Region: "us-east-1", Bucket: bucket, Prefix: multiKey,
		Mode: "stream", Format: "ndjson", MaxObjectBytes: 1 << 20,
		Endpoint: os.Getenv("S3_ENDPOINT"), ForcePathStyle: true,
		AccessKey: os.Getenv("S3_ACCESS_KEY"), SecretKey: os.Getenv("S3_SECRET_KEY"), Timeout: 10 * time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := multi.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer multi.Stop(context.Background())
	facts := make(chan *adapters.TypedFact, 100)
	errCh := make(chan error, 1)
	go func() { errCh <- multi.pollOnce(t.Context(), facts) }()
	time.Sleep(100 * time.Millisecond) // force the producer to block on channel capacity
	for i := 0; i < 150; i++ {
		select {
		case fact := <-facts:
			if fact.Acknowledge == nil {
				t.Fatalf("record %d has no acknowledgement", i)
			}
			if err := fact.Acknowledge(t.Context()); err != nil {
				t.Fatalf("acknowledge record %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("received %d/150 records", i)
		}
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if multi.lastSeenKey != multiKey {
		t.Fatalf("cursor did not advance after all records: %q", multi.lastSeenKey)
	}
}
