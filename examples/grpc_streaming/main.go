package main

import (
	"context"
	"log"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/grpc"
)

const (
	grpcAddress = "localhost:9000"
	grpcMethod  = "/acme.v1.Facts/StreamFacts"
)

type factsService struct{}

func streamFactsHandler(srv interface{}, stream grpc.ServerStream) error {
	req := &structpb.Struct{}
	if err := stream.RecvMsg(req); err != nil {
		return err
	}

	for i := 1; i <= 3; i++ {
		payload := map[string]interface{}{
			"type":   "example.event",
			"seq":    i,
			"source": "grpc",
		}
		resp, err := structpb.NewStruct(payload)
		if err != nil {
			return err
		}
		if err := stream.SendMsg(resp); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil
}

func main() {
	lis, err := net.Listen("tcp", grpcAddress)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	server := grpc.NewServer()
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: "acme.v1.Facts",
		HandlerType: (*factsService)(nil),
		Streams: []grpc.StreamDesc{
			{
				StreamName:    "StreamFacts",
				Handler:       streamFactsHandler,
				ServerStreams: true,
			},
		},
	}, &factsService{})

	go server.Serve(lis)
	defer server.GracefulStop()

	time.Sleep(200 * time.Millisecond)

	config := adapters.SourceConfig{
		SourceID: "grpc_example",
		Type:     "grpc",
		Config: map[string]interface{}{
			"address":         grpcAddress,
			"method":          grpcMethod,
			"tls":             false,
			"schema_name":     "example.grpc",
			"fact_type_field": "type",
		},
	}

	source, err := adapters.CreateSource(config)
	if err != nil {
		log.Fatalf("create source: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	facts, err := source.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe: %v", err)
	}
	defer source.Stop(ctx)

	for i := 0; i < 3; i++ {
		select {
		case fact := <-facts:
			log.Printf("fact schema=%s source=%s", fact.SchemaName, fact.SourceID)
			log.Printf("raw=%s", string(fact.RawData))
		case <-ctx.Done():
			log.Printf("timeout waiting for fact")
			return
		}
	}
}
