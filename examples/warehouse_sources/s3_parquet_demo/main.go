package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/s3"
	"google.golang.org/protobuf/types/known/structpb"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	endpoint := envOrDefault("S3_ENDPOINT", "http://localhost:9000")
	region := envOrDefault("S3_REGION", "us-east-1")
	bucket := envOrDefault("S3_BUCKET", "exports")
	prefix := envOrDefault("S3_PREFIX", "parquet/")
	accessKey := envOrDefault("S3_ACCESS_KEY", "minioadmin")
	secretKey := envOrDefault("S3_SECRET_KEY", "minioadmin")
	forcePathStyle := envBool("S3_FORCE_PATH_STYLE", true)

	config := adapters.SourceConfig{
		SourceID: "s3_parquet_demo",
		Type:     "s3",
		Config: map[string]interface{}{
			"region":           region,
			"bucket":           bucket,
			"prefix":           prefix,
			"mode":             "batch",
			"format":           "parquet",
			"poll_interval":    "1h",
			"endpoint":         endpoint,
			"force_path_style": forcePathStyle,
			"access_key":       accessKey,
			"secret_key":       secretKey,
			"schema_name":      "demo.s3.event",
			"max_objects":      5,
		},
	}

	source, err := adapters.CreateSource(config)
	if err != nil {
		log.Fatalf("create source: %v", err)
	}

	if err := source.Start(ctx); err != nil {
		log.Fatalf("start source: %v", err)
	}
	defer source.Stop(ctx)

	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}

	count := 0
	for {
		select {
		case fact, ok := <-facts:
			if !ok {
				log.Println("fact channel closed")
				return
			}
			count++
			fmt.Printf("Fact %d: schema=%s source=%s\n", count, fact.SchemaName, fact.SourceID)
			fmt.Printf("  metadata: %v\n", fact.Metadata)
			fmt.Printf("  payload: %s\n", prettyProto(fact.Data))
			if count >= 3 {
				return
			}
		case <-ctx.Done():
			log.Println("timeout waiting for facts")
			return
		}
	}
}

func prettyProto(msg interface{}) string {
	if msg == nil {
		return "null"
	}
	if data, ok := msg.(*structpb.Struct); ok {
		raw, _ := json.MarshalIndent(data.AsMap(), "", "  ")
		return string(raw)
	}
	raw, _ := json.MarshalIndent(msg, "", "  ")
	return string(raw)
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
