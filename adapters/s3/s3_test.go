package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/effectus/effectus-go/adapters"
)

type fakeS3 struct {
	mu       sync.Mutex
	pages    [][]types.Object
	payloads map[string][]byte
	failOnce map[string]bool
	lists    int
}

func (f *fakeS3) ListObjectsV2(ctx context.Context, input *awss3.ListObjectsV2Input, opts ...func(*awss3.Options)) (*awss3.ListObjectsV2Output, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	page := f.lists
	f.lists++
	if page >= len(f.pages) {
		return &awss3.ListObjectsV2Output{}, nil
	}
	out := &awss3.ListObjectsV2Output{Contents: f.pages[page]}
	if page+1 < len(f.pages) {
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String("next")
	}
	return out, nil
}

func (f *fakeS3) GetObject(ctx context.Context, input *awss3.GetObjectInput, opts ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := aws.ToString(input.Key)
	if f.failOnce[key] {
		f.failOnce[key] = false
		return nil, errors.New("transient get failure")
	}
	return &awss3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.payloads[key]))}, nil
}

func (f *fakeS3) HeadBucket(context.Context, *awss3.HeadBucketInput, ...func(*awss3.Options)) (*awss3.HeadBucketOutput, error) {
	return &awss3.HeadBucketOutput{}, nil
}

func object(key string, modified time.Time) types.Object {
	return types.Object{Key: aws.String(key), LastModified: aws.Time(modified), Size: aws.Int64(2)}
}

func newTestSource(t *testing.T, client s3API, format string, max int) *Source {
	t.Helper()
	source, err := NewSource(&Config{
		SourceID: "test", Region: "us-east-1", Bucket: "bucket", Mode: "stream",
		Format: format, MaxObjects: max, MaxObjectBytes: 1024, Timeout: time.Second,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.client = client
	return source
}

func TestStreamScansAllPagesBeforeMaxObjects(t *testing.T) {
	base := time.Unix(100, 0).UTC()
	client := &fakeS3{
		pages: [][]types.Object{
			{object("old-a", base.Add(-time.Hour)), object("old-b", base.Add(-time.Minute))},
			{object("new-c", base.Add(time.Second)), object("new-d", base.Add(2*time.Second)), object("new-e", base.Add(3*time.Second))},
		},
		payloads: map[string][]byte{"new-c": []byte(`{"id":3}`), "new-d": []byte(`{"id":4}`), "new-e": []byte(`{"id":5}`)},
		failOnce: map[string]bool{},
	}
	source := newTestSource(t, client, "json", 2)
	source.lastSeenTime = base
	out := make(chan *adapters.TypedFact, 2)
	if err := source.pollOnce(t.Context(), out); err != nil {
		t.Fatal(err)
	}
	if client.lists != 2 {
		t.Fatalf("listed %d pages, want 2", client.lists)
	}
	if got := len(out); got != 2 {
		t.Fatalf("emitted %d facts, want 2", got)
	}
	if source.lastSeenKey != "new-d" {
		t.Fatalf("cursor key = %q, want new-d", source.lastSeenKey)
	}
}

func TestStreamStopsAtFailedObjectAndRetriesFromCursor(t *testing.T) {
	base := time.Unix(200, 0).UTC()
	objects := []types.Object{object("a", base), object("b", base.Add(time.Second)), object("c", base.Add(2*time.Second))}
	client := &fakeS3{
		pages:    [][]types.Object{objects},
		payloads: map[string][]byte{"a": []byte(`{"id":"a"}`), "b": []byte(`{"id":"b"}`), "c": []byte(`{"id":"c"}`)},
		failOnce: map[string]bool{"b": true},
	}
	source := newTestSource(t, client, "json", 0)
	out := make(chan *adapters.TypedFact, 4)
	if err := source.pollOnce(t.Context(), out); err == nil {
		t.Fatal("expected first poll to fail")
	}
	if source.lastSeenKey != "a" {
		t.Fatalf("cursor advanced to %q after b failed", source.lastSeenKey)
	}
	client.mu.Lock()
	client.pages = [][]types.Object{objects}
	client.lists = 0
	client.mu.Unlock()
	if err := source.pollOnce(t.Context(), out); err != nil {
		t.Fatal(err)
	}
	if source.lastSeenKey != "c" {
		t.Fatalf("cursor key = %q, want c", source.lastSeenKey)
	}
	if got := len(out); got != 3 { // a once, then b and c
		t.Fatalf("emitted %d facts, want 3", got)
	}
}

func TestOversizedObjectIsAnErrorAndDoesNotAdvanceCursor(t *testing.T) {
	now := time.Now().UTC()
	client := &fakeS3{pages: [][]types.Object{{object("large", now)}}, payloads: map[string][]byte{}, failOnce: map[string]bool{}}
	client.pages[0][0].Size = aws.Int64(2048)
	source := newTestSource(t, client, "json", 0)
	source.config.MaxObjectBytes = 1024
	if err := source.pollOnce(t.Context(), make(chan *adapters.TypedFact, 1)); err == nil {
		t.Fatal("expected oversized object error")
	}
	if !source.lastSeenTime.IsZero() || source.lastSeenKey != "" {
		t.Fatal("cursor advanced past oversized object")
	}
}

func TestMultiRecordObjectCancellationDoesNotAdvanceCursor(t *testing.T) {
	now := time.Now().UTC()
	client := &fakeS3{
		pages:    [][]types.Object{{object("records", now)}},
		payloads: map[string][]byte{"records": []byte("{\"id\":1}\n{\"id\":2}\n")},
		failOnce: map[string]bool{},
	}
	source := newTestSource(t, client, "ndjson", 0)
	out := make(chan *adapters.TypedFact)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- source.pollOnce(ctx, out) }()
	<-out
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("expected cancellation error")
	}
	if !source.lastSeenTime.IsZero() || source.lastSeenKey != "" {
		t.Fatalf("cursor advanced after partial object: %v/%q", source.lastSeenTime, source.lastSeenKey)
	}
}
