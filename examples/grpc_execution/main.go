package main

import (
	"context"
	"fmt"
	"log"
	"time"

	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// This example calls the generated stable execution service. Plaintext
// transport is suitable only for this local example; production clients must
// use TLS credentials.
func main() {
	connection, err := grpc.NewClient("127.0.0.1:8081", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	facts, err := structpb.NewStruct(map[string]any{"order_id": "42", "amount": 100})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("authorization", "Bearer local-demo-token"))
	response, err := effectusv1.NewRulesetExecutionServiceClient(connection).ExecuteRuleset(ctx, &effectusv1.ExecutionRequest{
		RulesetName: "orders", Version: "1.0.0", Namespace: "demo", IdempotencyKey: "order-42",
		TypedFacts: facts, WaitMode: effectusv1.ExecutionWaitMode_EXECUTION_WAIT_MODE_TERMINAL,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("execution=%s state=%s generation=%s\n", response.ExecutionId, response.Metadata["state"], response.Metadata["generation_digest"])
}
