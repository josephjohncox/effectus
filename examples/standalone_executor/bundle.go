// bundle.go creates the source bundle used by the standalone order-review demo.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/josephjohncox/effectus/bundle"
	"github.com/josephjohncox/effectus/invocation"
	"github.com/josephjohncox/effectus/ir"
)

func main() {
	if len(os.Args) != 3 && len(os.Args) != 4 {
		panic("usage: go run bundle.go OUTPUT EXECUTOR_TOKEN [VERSION]")
	}
	version := "1.0.0"
	if len(os.Args) == 4 {
		version = os.Args[3]
	}
	_, file, _, ok := goruntime.Caller(0)
	if !ok {
		panic("resolve bundle generator path")
	}
	rule, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "order_review", "rules", "order_review.eff"))
	if err != nil {
		panic(err)
	}
	forward, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: invocation.HTTPResolverID, Reference: "http://business-executor:8090/reviews", Headers: map[string]string{"X-Demo-Token": os.Args[2]}, Settings: map[string]string{"method": "POST", "timeout": "5s", "allow_private_network": "true"}})
	if err != nil {
		panic(err)
	}
	inverse, err := invocation.NewDescriptor(invocation.DescriptorSpec{Type: invocation.DescriptorHTTP, ResolverID: invocation.HTTPResolverID, Reference: "http://business-executor:8090/reviews/cancel", Headers: map[string]string{"X-Demo-Token": os.Args[2]}, Settings: map[string]string{"method": "POST", "timeout": "5s", "allow_private_network": "true"}})
	if err != nil {
		panic(err)
	}
	contract := ir.VerbContract{Arguments: map[string]string{"orderId": "string", "reason": "string"}, RequiredArgs: []string{"orderId", "reason"}, ResultType: "string", InverseVerb: "CancelManualReview", IdempotencyPolicy: ir.IdempotencySinkGuaranteed, RetryPolicy: ir.RetryPolicy{MaxAttempts: 3, InitialBackoffMillis: 100, MaxBackoffMillis: 1000}}
	cancel := contract
	cancel.InverseVerb = "RequestManualReview"
	source, err := bundle.New(bundle.Spec{Name: "order-review", Version: version, Sources: []bundle.Source{{Path: "rules/order_review.eff", Content: string(rule)}}, Environment: ir.Environment{Facts: map[string]string{"order.id": "string", "order.total": "float", "order.risk_score": "int"}, Verbs: map[string]ir.VerbContract{"RequestManualReview": contract, "CancelManualReview": cancel}}, Executors: map[string]invocation.Descriptor{"RequestManualReview": forward, "CancelManualReview": inverse}})
	if err != nil {
		panic(err)
	}
	data, err := source.Bytes()
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile(os.Args[1], data, 0o600); err != nil {
		panic(err)
	}
	fmt.Println(os.Args[1])
}
