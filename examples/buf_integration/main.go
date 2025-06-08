package main

import (
	"context"
	"fmt"
	"log"

	"github.com/effectus/effectus-go/schema"
)

func main() {
	ctx := context.Background()

	// Initialize Buf integration
	bufIntegration, err := schema.NewBufIntegration("../../")
	if err != nil {
		log.Fatalf("Failed to initialize Buf integration: %v", err)
	}

	fmt.Println("🚀 Effectus Buf Integration Example")
	fmt.Println("===================================")

	// Example 1: Register a verb schema
	fmt.Println("\n1. Registering verb schema...")
	verbSchema := &schema.VerbSchema{
		Name:        "send_notification",
		Version:     "1.0.0",
		Description: "Send a notification to a user",
		InputSchema: map[string]interface{}{
			"user_id": "string",
			"message": "string",
			"channel": "string", // email, sms, push
			"urgent":  "boolean",
		},
		OutputSchema: map[string]interface{}{
			"notification_id": "string",
			"status":          "string",
			"delivered_at":    "timestamp",
		},
		RequiredCapabilities: []string{"notification.send"},
		ExecutionType:        "HTTP",
		Idempotent:           false,
		Compensatable:        true,
		BufModule:            "buf.build/effectus/effectus",
	}

	if err := bufIntegration.RegisterVerbSchema(ctx, verbSchema); err != nil {
		log.Fatalf("Failed to register verb schema: %v", err)
	}
	fmt.Printf("✅ Registered verb schema: %s v%s\n", verbSchema.Name, verbSchema.Version)

	// Example 2: Register a fact schema
	fmt.Println("\n2. Registering fact schema...")
	factSchema := &schema.FactSchema{
		Name:        "user_activity",
		Version:     "1.0.0",
		Description: "User activity tracking data",
		Schema: map[string]interface{}{
			"user_id":       "string",
			"activity_type": "string",
			"timestamp":     "timestamp",
			"metadata":      "map<string,string>",
			"session_id":    "string",
		},
		Indexes: []schema.IndexDefinition{
			{
				Name:   "user_id_timestamp",
				Fields: []string{"user_id", "timestamp"},
				Type:   "BTREE",
				Unique: false,
				Sparse: false,
			},
			{
				Name:   "session_id",
				Fields: []string{"session_id"},
				Type:   "HASH",
				Unique: false,
				Sparse: true,
			},
		},
		RetentionPolicy: &schema.RetentionPolicy{
			Duration: "90d",
			Strategy: "TIME_BASED",
		},
		PrivacyRules: []schema.PrivacyRule{
			{
				FieldPath:    "metadata.email",
				Action:       "MASK",
				MaskPattern:  "***@***.***",
				AllowedRoles: []string{"admin", "analyst"},
			},
		},
		BufModule: "buf.build/effectus/effectus",
	}

	if err := bufIntegration.RegisterFactSchema(ctx, factSchema); err != nil {
		log.Fatalf("Failed to register fact schema: %v", err)
	}
	fmt.Printf("✅ Registered fact schema: %s v%s\n", factSchema.Name, factSchema.Version)

	// Example 3: Generate code
	fmt.Println("\n3. Generating protobuf code...")
	result, err := bufIntegration.GenerateCode(ctx)
	if err != nil {
		log.Fatalf("Failed to generate code: %v", err)
	}

	if result.Success {
		fmt.Printf("✅ Code generation completed in %v\n", result.Duration)
		fmt.Printf("📁 Generated %d files:\n", len(result.GeneratedFiles))
		for _, file := range result.GeneratedFiles {
			fmt.Printf("   - %s\n", file)
		}
	} else {
		fmt.Printf("❌ Code generation failed:\n")
		for _, err := range result.Errors {
			fmt.Printf("   - %s\n", err)
		}
	}

	// Example 4: Validate schemas
	fmt.Println("\n4. Validating schemas...")
	validation, err := bufIntegration.ValidateSchemas(ctx)
	if err != nil {
		log.Printf("Schema validation encountered issues: %v", err)
	}

	if validation.Valid {
		fmt.Println("✅ All schemas are valid")
	} else {
		fmt.Println("❌ Schema validation failed:")
		for _, err := range validation.Errors {
			fmt.Printf("   - %s\n", err)
		}
	}

	if len(validation.BreakingChanges) > 0 {
		fmt.Println("⚠️  Breaking changes detected:")
		for _, change := range validation.BreakingChanges {
			fmt.Printf("   - %s\n", change)
		}
	}

	if len(validation.Warnings) > 0 {
		fmt.Println("⚠️  Warnings:")
		for _, warning := range validation.Warnings {
			fmt.Printf("   - %s\n", warning)
		}
	}

	// Example 5: List registered schemas
	fmt.Println("\n5. Listing registered schemas...")

	verbSchemas := bufIntegration.ListVerbSchemas()
	fmt.Printf("📋 Verb schemas (%d):\n", len(verbSchemas))
	for name, schema := range verbSchemas {
		fmt.Printf("   - %s v%s: %s\n", name, schema.Version, schema.Description)
	}

	factSchemas := bufIntegration.ListFactSchemas()
	fmt.Printf("📋 Fact schemas (%d):\n", len(factSchemas))
	for name, schema := range factSchemas {
		fmt.Printf("   - %s v%s: %s\n", name, schema.Version, schema.Description)
	}

	// Example 6: Schema evolution example
	fmt.Println("\n6. Schema evolution example...")

	// Create a new version of the verb schema with additional field
	evolvedVerbSchema := &schema.VerbSchema{
		Name:        "send_notification",
		Version:     "1.1.0",
		Description: "Send a notification to a user (with priority support)",
		InputSchema: map[string]interface{}{
			"user_id":  "string",
			"message":  "string",
			"channel":  "string",
			"urgent":   "boolean",
			"priority": "integer", // New field
		},
		OutputSchema: map[string]interface{}{
			"notification_id": "string",
			"status":          "string",
			"delivered_at":    "timestamp",
			"priority_used":   "integer", // New field
		},
		RequiredCapabilities: []string{"notification.send"},
		ExecutionType:        "HTTP",
		Idempotent:           false,
		Compensatable:        true,
		BufModule:            "buf.build/effectus/effectus",
	}

	if err := bufIntegration.RegisterVerbSchema(ctx, evolvedVerbSchema); err != nil {
		log.Printf("Schema evolution failed: %v", err)
	} else {
		fmt.Printf("✅ Evolved verb schema to v%s\n", evolvedVerbSchema.Version)
	}

	// Example 7: Demonstrate compatibility checking
	fmt.Println("\n7. Compatibility checking...")
	if originalSchema, exists := bufIntegration.GetVerbSchema("send_notification"); exists {
		fmt.Printf("📊 Current schema version: %s\n", originalSchema.Version)
		fmt.Printf("📊 Schema module: %s\n", originalSchema.BufModule)
		fmt.Printf("📊 Execution type: %s\n", originalSchema.ExecutionType)
		fmt.Printf("📊 Required capabilities: %v\n", originalSchema.RequiredCapabilities)
	}

	fmt.Println("\n🎉 Buf integration example completed!")
	fmt.Println("\nNext steps:")
	fmt.Println("- Run 'just buf-generate' to generate multi-language clients")
	fmt.Println("- Run 'just buf-validate' to check for breaking changes")
	fmt.Println("- Run 'just buf-push' to publish schemas to Buf Schema Registry")
	fmt.Println("- Check generated clients in clients/ directory")
}

// Helper function to demonstrate schema validation
func demonstrateSchemaValidation(bufIntegration *schema.BufIntegration) {
	ctx := context.Background()

	// Create an incompatible schema update
	incompatibleSchema := &schema.VerbSchema{
		Name:        "send_notification",
		Version:     "2.0.0",
		Description: "Send a notification to a user (breaking changes)",
		InputSchema: map[string]interface{}{
			"recipient_id": "string", // Changed from user_id
			"content":      "string", // Changed from message
		},
		OutputSchema: map[string]interface{}{
			"id":     "string", // Changed from notification_id
			"result": "string", // Changed from status
		},
		RequiredCapabilities: []string{"notification.send.v2"}, // Different capability
		ExecutionType:        "GRPC",                           // Changed execution type
		Idempotent:           true,                             // Changed semantics
		Compensatable:        false,                            // Changed semantics
		BufModule:            "buf.build/effectus/effectus",
	}

	fmt.Println("\n🔬 Testing incompatible schema update...")
	if err := bufIntegration.RegisterVerbSchema(ctx, incompatibleSchema); err != nil {
		fmt.Printf("❌ Expected failure for incompatible schema: %v\n", err)
	} else {
		fmt.Println("⚠️  Warning: Incompatible schema was accepted (this shouldn't happen)")
	}
}

// Helper function to generate sample schemas
func generateSampleSchemas() ([]*schema.VerbSchema, []*schema.FactSchema) {
	verbs := []*schema.VerbSchema{
		{
			Name:        "send_email",
			Version:     "1.0.0",
			Description: "Send an email message",
			InputSchema: map[string]interface{}{
				"to":      "string",
				"subject": "string",
				"body":    "string",
				"from":    "string",
			},
			OutputSchema: map[string]interface{}{
				"message_id": "string",
				"status":     "string",
			},
			RequiredCapabilities: []string{"email.send"},
			ExecutionType:        "HTTP",
			Idempotent:           true,
			Compensatable:        false,
		},
		{
			Name:        "http_request",
			Version:     "1.0.0",
			Description: "Make an HTTP request",
			InputSchema: map[string]interface{}{
				"url":     "string",
				"method":  "string",
				"headers": "map<string,string>",
				"body":    "string",
			},
			OutputSchema: map[string]interface{}{
				"status_code": "integer",
				"headers":     "map<string,string>",
				"body":        "string",
			},
			RequiredCapabilities: []string{"http.request"},
			ExecutionType:        "LOCAL",
			Idempotent:           false,
			Compensatable:        false,
		},
	}

	facts := []*schema.FactSchema{
		{
			Name:        "user_profile",
			Version:     "1.0.0",
			Description: "User profile information",
			Schema: map[string]interface{}{
				"user_id":     "string",
				"email":       "string",
				"name":        "string",
				"created_at":  "timestamp",
				"preferences": "map<string,string>",
			},
			Indexes: []schema.IndexDefinition{
				{Name: "user_id", Fields: []string{"user_id"}, Type: "HASH", Unique: true},
				{Name: "email", Fields: []string{"email"}, Type: "HASH", Unique: true},
			},
			PrivacyRules: []schema.PrivacyRule{
				{FieldPath: "email", Action: "MASK", MaskPattern: "***@***.***"},
			},
		},
		{
			Name:        "system_event",
			Version:     "1.0.0",
			Description: "System event tracking",
			Schema: map[string]interface{}{
				"event_id":   "string",
				"event_type": "string",
				"timestamp":  "timestamp",
				"payload":    "map<string,string>",
				"source":     "string",
			},
			Indexes: []schema.IndexDefinition{
				{Name: "timestamp", Fields: []string{"timestamp"}, Type: "BTREE"},
				{Name: "event_type", Fields: []string{"event_type"}, Type: "HASH"},
			},
			RetentionPolicy: &schema.RetentionPolicy{
				Duration: "30d",
				Strategy: "TIME_BASED",
			},
		},
	}

	return verbs, facts
}
