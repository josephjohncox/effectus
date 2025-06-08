# Effectus gRPC Client Examples

This document provides comprehensive examples for using the Effectus gRPC execution interface from different programming languages.

## Overview

The Effectus gRPC interface provides a standard **Facts → Effects** execution model where:

1. **Facts**: Typed input data (protobuf messages)
2. **Rulesets**: Named collections of business rules  
3. **Effects**: Typed output actions (protobuf messages)
4. **Execution**: Stateless, deterministic rule processing

## Go Client

### Setup

```bash
go mod init effectus-client
go get google.golang.org/grpc
go get google.golang.org/protobuf
```

### Basic Execution

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"
    
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/protobuf/types/known/anypb"
    "google.golang.org/protobuf/types/known/structpb"
    
    pb "github.com/effectus/effectus-go/runtime" // Generated protobuf
)

func main() {
    // Connect to Effectus server
    conn, err := grpc.Dial("localhost:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        log.Fatalf("Failed to connect: %v", err)
    }
    defer conn.Close()
    
    client := pb.NewRulesetExecutionServiceClient(conn)
    
    // Execute customer management rules
    if err := executeCustomerRules(client); err != nil {
        log.Fatalf("Customer rules failed: %v", err)
    }
    
    // Execute payment processing rules
    if err := executePaymentRules(client); err != nil {
        log.Fatalf("Payment rules failed: %v", err)
    }
}

func executeCustomerRules(client pb.RulesetExecutionServiceClient) error {
    // Create customer facts
    customerFacts := map[string]interface{}{
        "customer_id":       "cust-12345",
        "email":             "john.doe@example.com",
        "account_status":    "Active",
        "credit_score":      750,
        "registration_date": "2024-01-15T10:30:00Z",
        "tier":              "gold",
    }
    
    // Convert to protobuf Any
    factsStruct, err := structpb.NewStruct(customerFacts)
    if err != nil {
        return fmt.Errorf("failed to create facts struct: %w", err)
    }
    
    factsAny, err := anypb.New(factsStruct)
    if err != nil {
        return fmt.Errorf("failed to create facts Any: %w", err)
    }
    
    // Execute ruleset
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    response, err := client.ExecuteRuleset(ctx, &pb.ExecutionRequest{
        RulesetName: "customer_management",
        Version:     "1.0.0",
        Facts:       factsAny,
        Options: &pb.ExecutionOptions{
            DryRun:           false,
            MaxEffects:       10,
            TimeoutSeconds:   30,
            EnableTracing:    true,
            CapabilityFilter: []string{"send_email", "update_customer"},
        },
        TraceId: "trace-customer-" + fmt.Sprintf("%d", time.Now().Unix()),
    })
    
    if err != nil {
        return fmt.Errorf("execution failed: %w", err)
    }
    
    // Process results
    fmt.Printf("✅ Customer Management Execution:\n")
    fmt.Printf("   Success: %t\n", response.Success)
    fmt.Printf("   Execution ID: %s\n", response.ExecutionId)
    fmt.Printf("   Effects: %d\n", len(response.Effects))
    
    for i, effect := range response.Effects {
        fmt.Printf("   Effect %d: %s (%s)\n", i+1, effect.VerbName, effect.Status)
    }
    
    if len(response.Errors) > 0 {
        fmt.Printf("   Errors: %v\n", response.Errors)
    }
    
    return nil
}

func executePaymentRules(client pb.RulesetExecutionServiceClient) error {
    // Create payment facts
    paymentFacts := map[string]interface{}{
        "transaction_id": "txn-67890",
        "amount":         5000.0,
        "currency":       "USD",
        "customer_id":    "cust-12345",
        "payment_method": "credit_card",
        "risk_score":     0.3,
        "merchant_id":    "merchant-123",
    }
    
    factsStruct, _ := structpb.NewStruct(paymentFacts)
    factsAny, _ := anypb.New(factsStruct)
    
    ctx := context.Background()
    response, err := client.ExecuteRuleset(ctx, &pb.ExecutionRequest{
        RulesetName: "payment_processing",
        Version:     "2.1.0",
        Facts:       factsAny,
        Options: &pb.ExecutionOptions{
            EnableTracing: true,
        },
    })
    
    if err != nil {
        return err
    }
    
    fmt.Printf("✅ Payment Processing Execution:\n")
    fmt.Printf("   Success: %t\n", response.Success)
    fmt.Printf("   Effects: %d\n", len(response.Effects))
    
    return nil
}

// Streaming execution example
func streamingExecution(client pb.RulesetExecutionServiceClient) error {
    ctx := context.Background()
    
    factsStruct, _ := structpb.NewStruct(map[string]interface{}{
        "batch_id": "batch-123",
        "size":     1000,
    })
    factsAny, _ := anypb.New(factsStruct)
    
    stream, err := client.StreamExecution(ctx, &pb.ExecutionRequest{
        RulesetName: "batch_processing",
        Facts:       factsAny,
    })
    if err != nil {
        return err
    }
    
    fmt.Println("🔄 Streaming execution updates:")
    for {
        update, err := stream.Recv()
        if err != nil {
            break
        }
        
        fmt.Printf("   Phase: %s, Progress: %d%%, Message: %s\n",
            update.Phase, update.ProgressPercent, update.Message)
    }
    
    return nil
}
```

### Ruleset Management

```go
func manageRulesets(client pb.RulesetExecutionServiceClient) error {
    ctx := context.Background()
    
    // List all rulesets
    listResp, err := client.ListRulesets(ctx, &pb.ListRulesetsRequest{})
    if err != nil {
        return err
    }
    
    fmt.Printf("📋 Available Rulesets (%d):\n", len(listResp.Rulesets))
    for _, ruleset := range listResp.Rulesets {
        fmt.Printf("   • %s (v%s) - %s\n", 
            ruleset.Name, ruleset.Version, ruleset.Description)
        fmt.Printf("     Rules: %d, Dependencies: %v\n", 
            ruleset.RuleCount, ruleset.Dependencies)
    }
    
    // Get detailed info for specific ruleset
    infoResp, err := client.GetRulesetInfo(ctx, &pb.RulesetInfoRequest{
        RulesetName: "customer_management",
    })
    if err != nil {
        return err
    }
    
    fmt.Printf("\n🔍 Customer Management Details:\n")
    fmt.Printf("   Schema: %s\n", infoResp.FactSchema.Name)
    fmt.Printf("   Required Fields: %v\n", infoResp.FactSchema.Required)
    fmt.Printf("   Capabilities: %v\n", infoResp.Capabilities)
    
    return nil
}
```

## Python Client

### Setup

```bash
pip install grpcio grpcio-tools protobuf
python -m grpc_tools.protoc --python_out=. --grpc_python_out=. effectus.proto
```

### Basic Execution

```python
import grpc
import time
from google.protobuf import struct_pb2, any_pb2

# Import generated stubs (you would generate these from the .proto file)
# from effectus_pb2_grpc import RulesetExecutionServiceStub
# from effectus_pb2 import ExecutionRequest, ExecutionOptions

class EffectusClient:
    def __init__(self, server_address="localhost:8080"):
        self.channel = grpc.insecure_channel(server_address)
        self.client = RulesetExecutionServiceStub(self.channel)
    
    def execute_customer_rules(self, customer_data):
        """Execute customer management rules"""
        
        # Create facts structure
        facts_struct = struct_pb2.Struct()
        facts_struct.update({
            "customer_id": customer_data["customer_id"],
            "email": customer_data["email"],
            "account_status": customer_data["account_status"],
            "credit_score": customer_data.get("credit_score", 0),
            "registration_date": customer_data["registration_date"],
            "tier": customer_data.get("tier", "standard")
        })
        
        # Pack into Any type
        facts_any = any_pb2.Any()
        facts_any.Pack(facts_struct)
        
        # Create execution request
        request = ExecutionRequest(
            ruleset_name="customer_management",
            version="1.0.0",
            facts=facts_any,
            options=ExecutionOptions(
                enable_tracing=True,
                max_effects=10,
                timeout_seconds=30,
                capability_filter=["send_email", "update_customer"]
            ),
            trace_id=f"trace-customer-{int(time.time())}"
        )
        
        try:
            response = self.client.ExecuteRuleset(request)
            
            print("✅ Customer Management Execution:")
            print(f"   Success: {response.success}")
            print(f"   Execution ID: {response.execution_id}")
            print(f"   Effects: {len(response.effects)}")
            
            for i, effect in enumerate(response.effects, 1):
                print(f"   Effect {i}: {effect.verb_name} ({effect.status})")
            
            if response.errors:
                print(f"   Errors: {list(response.errors)}")
                
            return response
            
        except grpc.RpcError as e:
            print(f"❌ Execution failed: {e.code()} - {e.details()}")
            return None
    
    def execute_payment_rules(self, payment_data):
        """Execute payment processing rules"""
        
        facts_struct = struct_pb2.Struct()
        facts_struct.update({
            "transaction_id": payment_data["transaction_id"],
            "amount": payment_data["amount"],
            "currency": payment_data["currency"],
            "customer_id": payment_data["customer_id"],
            "payment_method": payment_data["payment_method"],
            "risk_score": payment_data.get("risk_score", 0.0),
            "merchant_id": payment_data.get("merchant_id", "")
        })
        
        facts_any = any_pb2.Any()
        facts_any.Pack(facts_struct)
        
        request = ExecutionRequest(
            ruleset_name="payment_processing",
            version="2.1.0",
            facts=facts_any,
            options=ExecutionOptions(enable_tracing=True)
        )
        
        try:
            response = self.client.ExecuteRuleset(request)
            
            print("✅ Payment Processing Execution:")
            print(f"   Success: {response.success}")
            print(f"   Effects: {len(response.effects)}")
            
            for effect in response.effects:
                print(f"   {effect.verb_name}: {effect.status}")
                
            return response
            
        except grpc.RpcError as e:
            print(f"❌ Payment execution failed: {e}")
            return None
    
    def list_rulesets(self):
        """List all available rulesets"""
        
        try:
            request = ListRulesetsRequest()
            response = self.client.ListRulesets(request)
            
            print(f"📋 Available Rulesets ({len(response.rulesets)}):")
            for ruleset in response.rulesets:
                print(f"   • {ruleset.name} (v{ruleset.version}) - {ruleset.description}")
                print(f"     Rules: {ruleset.rule_count}, Dependencies: {list(ruleset.dependencies)}")
                
            return response.rulesets
            
        except grpc.RpcError as e:
            print(f"❌ Failed to list rulesets: {e}")
            return []
    
    def stream_execution(self, ruleset_name, facts_data):
        """Stream execution progress for long-running rules"""
        
        facts_struct = struct_pb2.Struct()
        facts_struct.update(facts_data)
        facts_any = any_pb2.Any()
        facts_any.Pack(facts_struct)
        
        request = ExecutionRequest(
            ruleset_name=ruleset_name,
            facts=facts_any
        )
        
        try:
            stream = self.client.StreamExecution(request)
            
            print("🔄 Streaming execution updates:")
            for update in stream:
                print(f"   Phase: {update.phase}, Progress: {update.progress_percent}%, "
                      f"Message: {update.message}")
                
        except grpc.RpcError as e:
            print(f"❌ Streaming failed: {e}")
    
    def close(self):
        """Close the connection"""
        self.channel.close()

# Usage example
def main():
    client = EffectusClient()
    
    try:
        # Execute customer rules
        customer_data = {
            "customer_id": "cust-12345",
            "email": "john.doe@example.com",
            "account_status": "Active",
            "credit_score": 750,
            "registration_date": "2024-01-15T10:30:00Z",
            "tier": "gold"
        }
        
        client.execute_customer_rules(customer_data)
        
        # Execute payment rules
        payment_data = {
            "transaction_id": "txn-67890",
            "amount": 5000.0,
            "currency": "USD",
            "customer_id": "cust-12345",
            "payment_method": "credit_card",
            "risk_score": 0.3,
            "merchant_id": "merchant-123"
        }
        
        client.execute_payment_rules(payment_data)
        
        # List available rulesets
        client.list_rulesets()
        
    finally:
        client.close()

if __name__ == "__main__":
    main()
```

### Async Python Client

```python
import asyncio
import grpc.aio
from google.protobuf import struct_pb2, any_pb2

class AsyncEffectusClient:
    def __init__(self, server_address="localhost:8080"):
        self.channel = grpc.aio.insecure_channel(server_address)
        self.client = RulesetExecutionServiceStub(self.channel)
    
    async def execute_ruleset_async(self, ruleset_name, facts_data, version="latest"):
        """Execute ruleset asynchronously"""
        
        facts_struct = struct_pb2.Struct()
        facts_struct.update(facts_data)
        facts_any = any_pb2.Any()
        facts_any.Pack(facts_struct)
        
        request = ExecutionRequest(
            ruleset_name=ruleset_name,
            version=version,
            facts=facts_any,
            options=ExecutionOptions(enable_tracing=True)
        )
        
        try:
            response = await self.client.ExecuteRuleset(request)
            return {
                "success": response.success,
                "execution_id": response.execution_id,
                "effects": [
                    {"verb": effect.verb_name, "status": effect.status}
                    for effect in response.effects
                ],
                "errors": list(response.errors)
            }
        except grpc.RpcError as e:
            return {"success": False, "error": str(e)}
    
    async def batch_execute(self, executions):
        """Execute multiple rulesets concurrently"""
        
        tasks = [
            self.execute_ruleset_async(
                exec_data["ruleset"],
                exec_data["facts"],
                exec_data.get("version", "latest")
            )
            for exec_data in executions
        ]
        
        results = await asyncio.gather(*tasks, return_exceptions=True)
        return results
    
    async def close(self):
        await self.channel.close()

# Usage
async def async_main():
    client = AsyncEffectusClient()
    
    try:
        # Concurrent execution of multiple rulesets
        executions = [
            {
                "ruleset": "customer_management",
                "facts": {"customer_id": "cust-1", "status": "Active"}
            },
            {
                "ruleset": "payment_processing", 
                "facts": {"transaction_id": "txn-1", "amount": 1000.0}
            },
            {
                "ruleset": "inventory_management",
                "facts": {"product_id": "prod-1", "quantity": 50}
            }
        ]
        
        results = await client.batch_execute(executions)
        
        for i, result in enumerate(results):
            print(f"Execution {i+1}: {result}")
            
    finally:
        await client.close()

# Run async example
# asyncio.run(async_main())
```

## TypeScript/Node.js Client

### Setup

```bash
npm init -y
npm install @grpc/grpc-js @grpc/proto-loader google-protobuf
npm install --save-dev @types/google-protobuf
```

### Basic Client

```typescript
import * as grpc from '@grpc/grpc-js';
import * as protoLoader from '@grpc/proto-loader';
import { Struct } from 'google-protobuf/google/protobuf/struct_pb';
import { Any } from 'google-protobuf/google/protobuf/any_pb';

interface CustomerFacts {
    customer_id: string;
    email: string;
    account_status: string;
    credit_score?: number;
    registration_date: string;
    tier?: string;
}

interface PaymentFacts {
    transaction_id: string;
    amount: number;
    currency: string;
    customer_id: string;
    payment_method: string;
    risk_score?: number;
    merchant_id?: string;
}

class EffectusClient {
    private client: any;
    
    constructor(serverAddress: string = 'localhost:8080') {
        // Load protobuf definition
        const packageDefinition = protoLoader.loadSync('effectus.proto', {
            keepCase: true,
            longs: String,
            enums: String,
            defaults: true,
            oneofs: true
        });
        
        const protoDescriptor = grpc.loadPackageDefinition(packageDefinition);
        const effectusProto = (protoDescriptor as any).effectus.runtime;
        
        this.client = new effectusProto.RulesetExecutionService(
            serverAddress,
            grpc.credentials.createInsecure()
        );
    }
    
    async executeCustomerRules(customerData: CustomerFacts): Promise<any> {
        return new Promise((resolve, reject) => {
            // Create facts structure
            const factsStruct = Struct.fromJavaScript({
                customer_id: customerData.customer_id,
                email: customerData.email,
                account_status: customerData.account_status,
                credit_score: customerData.credit_score || 0,
                registration_date: customerData.registration_date,
                tier: customerData.tier || 'standard'
            });
            
            // Pack into Any type
            const factsAny = new Any();
            factsAny.pack(factsStruct.serializeBinary(), 'google.protobuf.Struct');
            
            const request = {
                ruleset_name: 'customer_management',
                version: '1.0.0',
                facts: factsAny,
                options: {
                    enable_tracing: true,
                    max_effects: 10,
                    timeout_seconds: 30,
                    capability_filter: ['send_email', 'update_customer']
                },
                trace_id: `trace-customer-${Date.now()}`
            };
            
            this.client.ExecuteRuleset(request, (error: any, response: any) => {
                if (error) {
                    console.error('❌ Customer execution failed:', error);
                    reject(error);
                    return;
                }
                
                console.log('✅ Customer Management Execution:');
                console.log(`   Success: ${response.success}`);
                console.log(`   Execution ID: ${response.execution_id}`);
                console.log(`   Effects: ${response.effects.length}`);
                
                response.effects.forEach((effect: any, i: number) => {
                    console.log(`   Effect ${i+1}: ${effect.verb_name} (${effect.status})`);
                });
                
                resolve(response);
            });
        });
    }
    
    async executePaymentRules(paymentData: PaymentFacts): Promise<any> {
        return new Promise((resolve, reject) => {
            const factsStruct = Struct.fromJavaScript({
                transaction_id: paymentData.transaction_id,
                amount: paymentData.amount,
                currency: paymentData.currency,
                customer_id: paymentData.customer_id,
                payment_method: paymentData.payment_method,
                risk_score: paymentData.risk_score || 0.0,
                merchant_id: paymentData.merchant_id || ''
            });
            
            const factsAny = new Any();
            factsAny.pack(factsStruct.serializeBinary(), 'google.protobuf.Struct');
            
            const request = {
                ruleset_name: 'payment_processing',
                version: '2.1.0',
                facts: factsAny,
                options: {
                    enable_tracing: true
                }
            };
            
            this.client.ExecuteRuleset(request, (error: any, response: any) => {
                if (error) {
                    console.error('❌ Payment execution failed:', error);
                    reject(error);
                    return;
                }
                
                console.log('✅ Payment Processing Execution:');
                console.log(`   Success: ${response.success}`);
                console.log(`   Effects: ${response.effects.length}`);
                
                response.effects.forEach((effect: any) => {
                    console.log(`   ${effect.verb_name}: ${effect.status}`);
                });
                
                resolve(response);
            });
        });
    }
    
    async listRulesets(): Promise<any[]> {
        return new Promise((resolve, reject) => {
            this.client.ListRulesets({}, (error: any, response: any) => {
                if (error) {
                    console.error('❌ Failed to list rulesets:', error);
                    reject(error);
                    return;
                }
                
                console.log(`📋 Available Rulesets (${response.rulesets.length}):`);
                response.rulesets.forEach((ruleset: any) => {
                    console.log(`   • ${ruleset.name} (v${ruleset.version}) - ${ruleset.description}`);
                    console.log(`     Rules: ${ruleset.rule_count}, Dependencies: ${ruleset.dependencies}`);
                });
                
                resolve(response.rulesets);
            });
        });
    }
    
    async streamExecution(rulesetName: string, factsData: any): Promise<void> {
        const factsStruct = Struct.fromJavaScript(factsData);
        const factsAny = new Any();
        factsAny.pack(factsStruct.serializeBinary(), 'google.protobuf.Struct');
        
        const request = {
            ruleset_name: rulesetName,
            facts: factsAny
        };
        
        const stream = this.client.StreamExecution(request);
        
        console.log('🔄 Streaming execution updates:');
        
        stream.on('data', (update: any) => {
            console.log(`   Phase: ${update.phase}, Progress: ${update.progress_percent}%, Message: ${update.message}`);
        });
        
        stream.on('end', () => {
            console.log('   Stream completed');
        });
        
        stream.on('error', (error: any) => {
            console.error('❌ Stream error:', error);
        });
    }
    
    close(): void {
        // gRPC client cleanup if needed
    }
}

// Usage example
async function main() {
    const client = new EffectusClient();
    
    try {
        // Execute customer rules
        const customerData: CustomerFacts = {
            customer_id: 'cust-12345',
            email: 'john.doe@example.com',
            account_status: 'Active',
            credit_score: 750,
            registration_date: '2024-01-15T10:30:00Z',
            tier: 'gold'
        };
        
        await client.executeCustomerRules(customerData);
        
        // Execute payment rules
        const paymentData: PaymentFacts = {
            transaction_id: 'txn-67890',
            amount: 5000.0,
            currency: 'USD',
            customer_id: 'cust-12345',
            payment_method: 'credit_card',
            risk_score: 0.3,
            merchant_id: 'merchant-123'
        };
        
        await client.executePaymentRules(paymentData);
        
        // List available rulesets
        await client.listRulesets();
        
    } catch (error) {
        console.error('Application error:', error);
    } finally {
        client.close();
    }
}

// Run the example
main().catch(console.error);
```

## Java Client

### Setup

```xml
<!-- Maven dependencies -->
<dependencies>
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-netty-shaded</artifactId>
        <version>1.58.0</version>
    </dependency>
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-protobuf</artifactId>
        <version>1.58.0</version>
    </dependency>
    <dependency>
        <groupId>io.grpc</groupId>
        <artifactId>grpc-stub</artifactId>
        <version>1.58.0</version>
    </dependency>
    <dependency>
        <groupId>com.google.protobuf</groupId>
        <artifactId>protobuf-java</artifactId>
        <version>3.25.0</version>
    </dependency>
</dependencies>
```

### Basic Client

```java
import io.grpc.ManagedChannel;
import io.grpc.ManagedChannelBuilder;
import com.google.protobuf.Any;
import com.google.protobuf.Struct;
import com.google.protobuf.Value;
import java.util.concurrent.TimeUnit;

public class EffectusClient {
    private final ManagedChannel channel;
    private final RulesetExecutionServiceGrpc.RulesetExecutionServiceBlockingStub blockingStub;
    
    public EffectusClient(String target) {
        this.channel = ManagedChannelBuilder.forTarget(target)
                .usePlaintext()
                .build();
        this.blockingStub = RulesetExecutionServiceGrpc.newBlockingStub(channel);
    }
    
    public ExecutionResponse executeCustomerRules(CustomerData customerData) {
        // Create facts structure
        Struct.Builder factsBuilder = Struct.newBuilder();
        factsBuilder.putFields("customer_id", Value.newBuilder().setStringValue(customerData.getCustomerId()).build());
        factsBuilder.putFields("email", Value.newBuilder().setStringValue(customerData.getEmail()).build());
        factsBuilder.putFields("account_status", Value.newBuilder().setStringValue(customerData.getAccountStatus()).build());
        factsBuilder.putFields("credit_score", Value.newBuilder().setNumberValue(customerData.getCreditScore()).build());
        factsBuilder.putFields("registration_date", Value.newBuilder().setStringValue(customerData.getRegistrationDate()).build());
        factsBuilder.putFields("tier", Value.newBuilder().setStringValue(customerData.getTier()).build());
        
        Struct factsStruct = factsBuilder.build();
        Any factsAny = Any.pack(factsStruct);
        
        // Create execution request
        ExecutionRequest request = ExecutionRequest.newBuilder()
                .setRulesetName("customer_management")
                .setVersion("1.0.0")
                .setFacts(factsAny)
                .setOptions(ExecutionOptions.newBuilder()
                        .setEnableTracing(true)
                        .setMaxEffects(10)
                        .setTimeoutSeconds(30)
                        .addCapabilityFilter("send_email")
                        .addCapabilityFilter("update_customer")
                        .build())
                .setTraceId("trace-customer-" + System.currentTimeMillis())
                .build();
        
        try {
            ExecutionResponse response = blockingStub.executeRuleset(request);
            
            System.out.println("✅ Customer Management Execution:");
            System.out.println("   Success: " + response.getSuccess());
            System.out.println("   Execution ID: " + response.getExecutionId());
            System.out.println("   Effects: " + response.getEffectsCount());
            
            for (int i = 0; i < response.getEffectsCount(); i++) {
                TypedEffect effect = response.getEffects(i);
                System.out.println("   Effect " + (i+1) + ": " + effect.getVerbName() + " (" + effect.getStatus() + ")");
            }
            
            if (response.getErrorsCount() > 0) {
                System.out.println("   Errors: " + response.getErrorsList());
            }
            
            return response;
            
        } catch (Exception e) {
            System.err.println("❌ Execution failed: " + e.getMessage());
            return null;
        }
    }
    
    public void listRulesets() {
        ListRulesetsRequest request = ListRulesetsRequest.newBuilder().build();
        
        try {
            ListRulesetsResponse response = blockingStub.listRulesets(request);
            
            System.out.println("📋 Available Rulesets (" + response.getRulesetsCount() + "):");
            for (RulesetInfo ruleset : response.getRulesetsList()) {
                System.out.println("   • " + ruleset.getName() + " (v" + ruleset.getVersion() + ") - " + ruleset.getDescription());
                System.out.println("     Rules: " + ruleset.getRuleCount() + ", Dependencies: " + ruleset.getDependenciesList());
            }
            
        } catch (Exception e) {
            System.err.println("❌ Failed to list rulesets: " + e.getMessage());
        }
    }
    
    public void close() throws InterruptedException {
        channel.shutdown().awaitTermination(5, TimeUnit.SECONDS);
    }
    
    // Helper class for customer data
    public static class CustomerData {
        private String customerId;
        private String email;
        private String accountStatus;
        private int creditScore;
        private String registrationDate;
        private String tier;
        
        // Constructors, getters, and setters...
        public CustomerData(String customerId, String email, String accountStatus, 
                          int creditScore, String registrationDate, String tier) {
            this.customerId = customerId;
            this.email = email;
            this.accountStatus = accountStatus;
            this.creditScore = creditScore;
            this.registrationDate = registrationDate;
            this.tier = tier;
        }
        
        // Getters...
        public String getCustomerId() { return customerId; }
        public String getEmail() { return email; }
        public String getAccountStatus() { return accountStatus; }
        public int getCreditScore() { return creditScore; }
        public String getRegistrationDate() { return registrationDate; }
        public String getTier() { return tier; }
    }
    
    public static void main(String[] args) throws InterruptedException {
        EffectusClient client = new EffectusClient("localhost:8080");
        
        try {
            // Execute customer rules
            CustomerData customerData = new CustomerData(
                "cust-12345",
                "john.doe@example.com", 
                "Active",
                750,
                "2024-01-15T10:30:00Z",
                "gold"
            );
            
            client.executeCustomerRules(customerData);
            
            // List rulesets
            client.listRulesets();
            
        } finally {
            client.close();
        }
    }
}
```

## Common Integration Patterns

### Microservices Integration

```yaml
# docker-compose.yml for microservices setup
version: '3.8'
services:
  effectus:
    image: effectus:latest
    ports:
      - "8080:8080"
    environment:
      - GRPC_PORT=8080
      - HOT_RELOAD=true
  
  customer-service:
    build: ./customer-service
    depends_on:
      - effectus
    environment:
      - EFFECTUS_GRPC=effectus:8080
  
  payment-service:
    build: ./payment-service  
    depends_on:
      - effectus
    environment:
      - EFFECTUS_GRPC=effectus:8080
```

### Event-Driven Integration

```python
# Kafka consumer that triggers Effectus rules
import kafka
import json
from effectus_client import EffectusClient

def process_events():
    consumer = kafka.KafkaConsumer('customer-events', 'payment-events')
    effectus = EffectusClient()
    
    for message in consumer:
        event_data = json.loads(message.value)
        
        if message.topic == 'customer-events':
            effectus.execute_customer_rules(event_data)
        elif message.topic == 'payment-events':
            effectus.execute_payment_rules(event_data)
```

### API Gateway Integration

```javascript
// Express.js middleware for Effectus integration
const express = require('express');
const { EffectusClient } = require('./effectus-client');

const app = express();
const effectus = new EffectusClient();

app.post('/api/customers', async (req, res) => {
    try {
        // Process customer creation
        const customer = await createCustomer(req.body);
        
        // Trigger customer management rules
        const ruleResult = await effectus.executeCustomerRules({
            customer_id: customer.id,
            email: customer.email,
            account_status: 'Active',
            registration_date: new Date().toISOString()
        });
        
        res.json({ 
            customer,
            rule_execution: ruleResult 
        });
        
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});
```

## Best Practices

### Error Handling
- Always handle gRPC connection errors
- Check response.success before processing effects
- Log execution IDs for troubleshooting
- Implement retry logic with exponential backoff

### Performance
- Use connection pooling for high-throughput applications
- Enable gRPC compression for large fact payloads
- Implement caching for frequently executed rulesets
- Use streaming for long-running rule executions

### Security
- Use TLS for production deployments
- Implement proper authentication/authorization
- Validate input facts before sending to Effectus
- Filter sensitive data in logs and traces

### Monitoring
- Track execution success rates and latencies
- Monitor effect generation patterns
- Set up alerts for rule execution failures
- Use distributed tracing for complex workflows

This comprehensive set of client examples demonstrates how to integrate Effectus into applications using different programming languages while maintaining type safety and leveraging the full power of the gRPC execution interface. 