package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/effectus/effectus-go/runtime"
)

// CustomerRuleset demonstrates a complete customer management ruleset
func main() {
	fmt.Println("🎯 Effectus gRPC Execution Interface Demo")
	fmt.Println("=========================================")

	// 1. Create execution runtime
	execRuntime := runtime.NewExecutionRuntime()

	// 2. Create gRPC server
	grpcServer, err := runtime.NewRulesetExecutionServer(execRuntime, ":8080")
	if err != nil {
		log.Fatalf("Failed to create gRPC server: %v", err)
	}

	// Start gRPC server in background
	go func() {
		if err := grpcServer.Start(); err != nil {
			log.Printf("gRPC server error: %v", err)
		}
	}()
	defer grpcServer.Stop()

	// 3. Create and register customer management ruleset
	customerRuleset := createCustomerRuleset()
	if err := grpcServer.RegisterRuleset(customerRuleset); err != nil {
		log.Fatalf("Failed to register customer ruleset: %v", err)
	}

	// 4. Create and register payment processing ruleset
	paymentRuleset := createPaymentRuleset()
	if err := grpcServer.RegisterRuleset(paymentRuleset); err != nil {
		log.Fatalf("Failed to register payment ruleset: %v", err)
	}

	// 5. Enable hot reload
	grpcServer.EnableHotReload(30 * time.Second)

	// 6. Demonstrate execution via gRPC interface
	demonstrateExecution(grpcServer)

	// 7. Demonstrate ruleset management
	demonstrateRulesetManagement(grpcServer)

	fmt.Println("✅ Demo completed successfully!")
}

// createCustomerRuleset creates a customer management ruleset
func createCustomerRuleset() *runtime.CompiledRuleset {
	// Define customer fact schema
	factSchema := &runtime.Schema{
		Name: "CustomerFacts",
		Fields: map[string]*runtime.FieldType{
			"customer_id":       {Type: "string", Required: true, Description: "Unique customer identifier"},
			"email":             {Type: "string", Required: true, Description: "Customer email address"},
			"account_status":    {Type: "string", Required: true, Description: "Active, Suspended, Closed"},
			"credit_score":      {Type: "int32", Required: false, Description: "Customer credit score"},
			"registration_date": {Type: "string", Required: true, Description: "ISO date format"},
		},
		Required:    []string{"customer_id", "email", "account_status"},
		Description: "Customer management fact schema",
	}

	// Define effect schemas
	effectSchemas := map[string]*runtime.Schema{
		"send_welcome_email": {
			Name: "WelcomeEmailEffect",
			Fields: map[string]*runtime.FieldType{
				"to":       {Type: "string", Required: true, Description: "Recipient email"},
				"subject":  {Type: "string", Required: true, Description: "Email subject"},
				"template": {Type: "string", Required: true, Description: "Email template name"},
			},
			Required:    []string{"to", "subject", "template"},
			Description: "Welcome email effect schema",
		},
		"update_customer_status": {
			Name: "CustomerStatusEffect",
			Fields: map[string]*runtime.FieldType{
				"customer_id": {Type: "string", Required: true, Description: "Customer ID"},
				"status":      {Type: "string", Required: true, Description: "New status"},
				"reason":      {Type: "string", Required: false, Description: "Reason for change"},
			},
			Required:    []string{"customer_id", "status"},
			Description: "Customer status update effect schema",
		},
	}

	// Define compiled rules
	rules := []runtime.CompiledRule{
		{
			Name:        "new_customer_welcome",
			Type:        runtime.RuleTypeList,
			Description: "Send welcome email to new customers",
			Priority:    10,
			Predicates: []runtime.CompiledPredicate{
				{Path: "account_status", Operator: "==", Value: "Active"},
				{Path: "registration_date", Operator: "within", Value: "24h"},
			},
			Effects: []runtime.CompiledEffect{
				{
					VerbName: "send_welcome_email",
					Args: map[string]interface{}{
						"to":       "{{customer.email}}",
						"subject":  "Welcome to our service!",
						"template": "customer_welcome",
					},
				},
			},
		},
		{
			Name:        "suspended_customer_notification",
			Type:        runtime.RuleTypeList,
			Description: "Notify customers when account is suspended",
			Priority:    20,
			Predicates: []runtime.CompiledPredicate{
				{Path: "account_status", Operator: "==", Value: "Suspended"},
			},
			Effects: []runtime.CompiledEffect{
				{
					VerbName: "send_notification",
					Args: map[string]interface{}{
						"customer_id": "{{customer.customer_id}}",
						"type":        "account_suspended",
						"message":     "Your account has been temporarily suspended",
					},
				},
			},
		},
	}

	return &runtime.CompiledRuleset{
		Name:          "customer_management",
		Version:       "1.0.0",
		Description:   "Customer lifecycle management rules",
		FactSchema:    factSchema,
		EffectSchemas: effectSchemas,
		Rules:         rules,
		Dependencies:  []string{"email_service", "notification_service"},
		Capabilities:  []string{"send_email", "update_customer", "read_customer"},
		Metadata: map[string]string{
			"domain":     "customer_management",
			"team":       "customer_success",
			"created_by": "customer_team",
		},
	}
}

// createPaymentRuleset creates a payment processing ruleset
func createPaymentRuleset() *runtime.CompiledRuleset {
	// Define payment fact schema
	factSchema := &runtime.Schema{
		Name: "PaymentFacts",
		Fields: map[string]*runtime.FieldType{
			"transaction_id": {Type: "string", Required: true, Description: "Transaction identifier"},
			"amount":         {Type: "float64", Required: true, Description: "Payment amount"},
			"currency":       {Type: "string", Required: true, Description: "Currency code"},
			"customer_id":    {Type: "string", Required: true, Description: "Customer identifier"},
			"payment_method": {Type: "string", Required: true, Description: "Credit card, bank transfer, etc."},
			"risk_score":     {Type: "float64", Required: false, Description: "Fraud risk score"},
		},
		Required:    []string{"transaction_id", "amount", "currency", "customer_id"},
		Description: "Payment processing fact schema",
	}

	// Define payment effect schemas
	effectSchemas := map[string]*runtime.Schema{
		"process_payment": {
			Name: "PaymentProcessingEffect",
			Fields: map[string]*runtime.FieldType{
				"transaction_id": {Type: "string", Required: true, Description: "Transaction ID"},
				"amount":         {Type: "float64", Required: true, Description: "Amount to process"},
				"gateway":        {Type: "string", Required: true, Description: "Payment gateway"},
			},
			Required:    []string{"transaction_id", "amount", "gateway"},
			Description: "Payment processing effect schema",
		},
		"fraud_alert": {
			Name: "FraudAlertEffect",
			Fields: map[string]*runtime.FieldType{
				"transaction_id": {Type: "string", Required: true, Description: "Transaction ID"},
				"risk_score":     {Type: "float64", Required: true, Description: "Risk score"},
				"reason":         {Type: "string", Required: true, Description: "Alert reason"},
			},
			Required:    []string{"transaction_id", "risk_score", "reason"},
			Description: "Fraud alert effect schema",
		},
	}

	// Define payment rules
	rules := []runtime.CompiledRule{
		{
			Name:        "high_value_payment_review",
			Type:        runtime.RuleTypeFlow,
			Description: "Review high-value payments for fraud",
			Priority:    30,
			Predicates: []runtime.CompiledPredicate{
				{Path: "amount", Operator: ">", Value: 10000.0},
			},
			Effects: []runtime.CompiledEffect{
				{
					VerbName: "fraud_alert",
					Args: map[string]interface{}{
						"transaction_id": "{{payment.transaction_id}}",
						"risk_score":     "{{payment.risk_score}}",
						"reason":         "High value transaction requires review",
					},
				},
			},
		},
		{
			Name:        "process_normal_payment",
			Type:        runtime.RuleTypeList,
			Description: "Process normal payments automatically",
			Priority:    10,
			Predicates: []runtime.CompiledPredicate{
				{Path: "amount", Operator: "<=", Value: 10000.0},
				{Path: "risk_score", Operator: "<", Value: 0.7},
			},
			Effects: []runtime.CompiledEffect{
				{
					VerbName: "process_payment",
					Args: map[string]interface{}{
						"transaction_id": "{{payment.transaction_id}}",
						"amount":         "{{payment.amount}}",
						"gateway":        "primary_gateway",
					},
				},
			},
		},
	}

	return &runtime.CompiledRuleset{
		Name:          "payment_processing",
		Version:       "2.1.0",
		Description:   "Payment processing and fraud detection rules",
		FactSchema:    factSchema,
		EffectSchemas: effectSchemas,
		Rules:         rules,
		Dependencies:  []string{"payment_gateway", "fraud_detection"},
		Capabilities:  []string{"process_payment", "send_alert", "read_transaction"},
		Metadata: map[string]string{
			"domain":     "payments",
			"team":       "fintech",
			"created_by": "payments_team",
		},
	}
}

// demonstrateExecution shows how to execute rulesets via gRPC
func demonstrateExecution(server *runtime.RulesetExecutionServer) {
	fmt.Println("\n🚀 Demonstrating Ruleset Execution")
	fmt.Println("-----------------------------------")

	// Create customer facts
	customerFacts := map[string]interface{}{
		"customer_id":       "cust-12345",
		"email":             "john.doe@example.com",
		"account_status":    "Active",
		"credit_score":      750,
		"registration_date": "2024-01-15T10:30:00Z",
	}

	// Convert to protobuf Any
	factsStruct, err := structpb.NewStruct(customerFacts)
	if err != nil {
		log.Printf("Failed to create facts struct: %v", err)
		return
	}

	factsAny, err := anypb.New(factsStruct)
	if err != nil {
		log.Printf("Failed to create facts Any: %v", err)
		return
	}

	// Execute customer management ruleset
	req := &runtime.ExecutionRequest{
		RulesetName: "customer_management",
		Version:     "1.0.0",
		Facts:       factsAny,
		Options: &runtime.ExecutionOptions{
			DryRun:         false,
			MaxEffects:     10,
			TimeoutSeconds: 30,
			EnableTracing:  true,
		},
		TraceID: "trace-12345",
	}

	response, err := server.ExecuteRuleset(context.Background(), req)
	if err != nil {
		log.Printf("Execution failed: %v", err)
		return
	}

	fmt.Printf("✅ Customer ruleset execution successful:\n")
	fmt.Printf("   Execution ID: %s\n", response.ExecutionID)
	fmt.Printf("   Effects: %d\n", len(response.Effects))
	fmt.Printf("   Success: %t\n", response.Success)

	// Execute payment ruleset
	paymentFacts := map[string]interface{}{
		"transaction_id": "txn-67890",
		"amount":         5000.0,
		"currency":       "USD",
		"customer_id":    "cust-12345",
		"payment_method": "credit_card",
		"risk_score":     0.3,
	}

	paymentStruct, _ := structpb.NewStruct(paymentFacts)
	paymentAny, _ := anypb.New(paymentStruct)

	paymentReq := &runtime.ExecutionRequest{
		RulesetName: "payment_processing",
		Version:     "2.1.0",
		Facts:       paymentAny,
		Options: &runtime.ExecutionOptions{
			DryRun:        false,
			EnableTracing: true,
		},
	}

	paymentResponse, err := server.ExecuteRuleset(context.Background(), paymentReq)
	if err != nil {
		log.Printf("Payment execution failed: %v", err)
		return
	}

	fmt.Printf("✅ Payment ruleset execution successful:\n")
	fmt.Printf("   Execution ID: %s\n", paymentResponse.ExecutionID)
	fmt.Printf("   Effects: %d\n", len(paymentResponse.Effects))
	fmt.Printf("   Success: %t\n", paymentResponse.Success)
}

// demonstrateRulesetManagement shows ruleset management capabilities
func demonstrateRulesetManagement(server *runtime.RulesetExecutionServer) {
	fmt.Println("\n📋 Demonstrating Ruleset Management")
	fmt.Println("-----------------------------------")

	// List all rulesets
	rulesets, err := server.ListRulesets(context.Background())
	if err != nil {
		log.Printf("Failed to list rulesets: %v", err)
		return
	}

	fmt.Printf("📊 Registered Rulesets (%d total):\n", len(rulesets))
	for _, ruleset := range rulesets {
		fmt.Printf("   • %s (v%s) - %d rules, %d verbs\n",
			ruleset.Name, ruleset.Version, ruleset.RuleCount, ruleset.VerbCount)
		fmt.Printf("     Description: %s\n", ruleset.Description)
		fmt.Printf("     Dependencies: %v\n", ruleset.Dependencies)
		fmt.Printf("     Capabilities: %v\n", ruleset.Capabilities)
	}

	// Get detailed info for a specific ruleset
	info, err := server.GetRulesetInfo(context.Background(), "customer_management")
	if err != nil {
		log.Printf("Failed to get ruleset info: %v", err)
		return
	}

	fmt.Printf("\n🔍 Detailed Customer Management Ruleset Info:\n")
	fmt.Printf("   Name: %s\n", info.Name)
	fmt.Printf("   Version: %s\n", info.Version)
	fmt.Printf("   Description: %s\n", info.Description)
	fmt.Printf("   Fact Schema: %s\n", info.FactSchema.Name)
	fmt.Printf("   Required Fields: %v\n", info.FactSchema.Required)
	fmt.Printf("   Metadata: %v\n", info.Metadata)
}

// Example client code showing how external services would interact
func exampleClientUsage() {
	// This demonstrates how external services would use the gRPC interface
	fmt.Println("\n🔌 Example Client Usage")
	fmt.Println("----------------------")

	fmt.Println(`
// Go client example:
conn, err := grpc.Dial("localhost:8080", grpc.WithInsecure())
client := NewRulesetExecutionServiceClient(conn)

// Execute customer rules
response, err := client.ExecuteRuleset(ctx, &ExecutionRequest{
    RulesetName: "customer_management",
    Version:     "1.0.0", 
    Facts:       customerFactsAny,
    Options: &ExecutionOptions{
        EnableTracing: true,
        MaxEffects:    10,
    },
})

// Python client example:
import grpc
from effectus_pb2_grpc import RulesetExecutionServiceStub

channel = grpc.insecure_channel('localhost:8080')
client = RulesetExecutionServiceStub(channel)

response = client.ExecuteRuleset(
    ExecutionRequest(
        ruleset_name="payment_processing",
        facts=payment_facts_any,
        options=ExecutionOptions(enable_tracing=True)
    )
)
`)
}
