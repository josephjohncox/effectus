package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/effectus/effectus-go/adapters"
	"github.com/effectus/effectus-go/schema"
	"github.com/effectus/effectus-go/unified"
)

// main demonstrates the complete dynamic, multi-source Effectus system
func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initialize the dynamic system
	system, err := setupDynamicSystem()
	if err != nil {
		log.Fatalf("Failed to setup dynamic system: %v", err)
	}

	// Start the system
	if err := system.Start(ctx); err != nil {
		log.Fatalf("Failed to start system: %v", err)
	}

	log.Println("🚀 Effectus dynamic system started")
	log.Println("📊 Ingesting facts from multiple sources...")
	log.Println("🔄 Hot-reload enabled for schema updates...")
	log.Println("⚡ Ready to process rules dynamically!")

	// Wait for shutdown signal
	<-sigChan
	log.Println("🛑 Shutting down gracefully...")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := system.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}

	log.Println("✅ Shutdown complete")
}

func setupDynamicSystem() (*unified.DynamicSystem, error) {
	// System configuration
	config := &unified.SystemConfig{
		BufConfig: &schema.BufConfig{
			RegistryURL: "https://buf.build/effectus/effectus",
			Module:      "effectus/v1",
			Token:       os.Getenv("BUF_TOKEN"),
		},
		Capabilities: map[string]bool{
			"send_email":        true,
			"http_request":      true,
			"database_write":    true,
			"send_notification": true,
		},
		Sources: map[string]unified.SourceConfig{
			"kafka_user_events": {
				Type: "kafka",
				Config: map[string]interface{}{
					"brokers":        []string{"localhost:9092"},
					"topic":          "user.events",
					"consumer_group": "effectus_user_events",
					"schema_format":  "json", // Convert JSON to Proto
				},
			},
			"webhook_external": {
				Type: "http_webhook",
				Config: map[string]interface{}{
					"listen_port": 8081,
					"path":        "/webhook/external-events",
					"auth_method": "bearer_token",
				},
			},
			"postgres_changes": {
				Type: "postgres_cdc",
				Config: map[string]interface{}{
					"host":             "localhost",
					"port":             5432,
					"database":         "app_db",
					"replication_slot": "effectus_slot",
				},
			},
		},
	}

	// Create the dynamic system
	system, err := unified.NewDynamicSystem(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic system: %w", err)
	}

	// Register fact sources
	if err := registerFactSources(system, config); err != nil {
		return nil, fmt.Errorf("failed to register fact sources: %w", err)
	}

	// Register verb executors
	if err := registerVerbExecutors(system); err != nil {
		return nil, fmt.Errorf("failed to register verb executors: %w", err)
	}

	// Load initial rules
	if err := loadInitialRules(system); err != nil {
		return nil, fmt.Errorf("failed to load initial rules: %w", err)
	}

	return system, nil
}

func registerFactSources(system *unified.DynamicSystem, config *unified.SystemConfig) error {
	// 1. Kafka source for user events
	kafkaSource, err := adapters.NewKafkaFactSource(&adapters.KafkaSourceConfig{
		SourceID:      "kafka_user_events",
		Brokers:       []string{"localhost:9092"},
		Topic:         "user.events",
		ConsumerGroup: "effectus_user_events",
		SchemaFormat:  "json",
		FactMappings: map[string]string{
			"user.profile.v1": "acme.v1.facts.UserProfile",
			"user.session.v1": "acme.v1.facts.UserSession",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create Kafka source: %w", err)
	}

	if err := system.RegisterFactSource("kafka_user_events", kafkaSource); err != nil {
		return fmt.Errorf("failed to register Kafka source: %w", err)
	}

	// 2. HTTP webhook source for external API
	webhookSource, err := adapters.NewHTTPWebhookSource(&adapters.HTTPSourceConfig{
		SourceID:   "webhook_external",
		ListenPort: 8081,
		Path:       "/webhook/external-events",
		AuthMethod: "bearer_token",
		AuthConfig: map[string]string{
			"token_header":   "X-API-Key",
			"expected_token": os.Getenv("EXTERNAL_API_TOKEN"),
		},
		Transformations: []adapters.Transformation{
			{
				SourcePath: "$.user",
				TargetType: "acme.v1.facts.UserProfile",
				Mapping: map[string]string{
					"user_id": "$.id",
					"email":   "$.email_address",
					"name":    "$.full_name",
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create webhook source: %w", err)
	}

	if err := system.RegisterFactSource("webhook_external", webhookSource); err != nil {
		return fmt.Errorf("failed to register webhook source: %w", err)
	}

	// 3. PostgreSQL change data capture
	postgresSource, err := adapters.NewPostgresChangeStreamSource(&adapters.PostgresSourceConfig{
		SourceID:        "postgres_changes",
		Host:            "localhost",
		Port:            5432,
		Database:        "app_db",
		Username:        "effectus_reader",
		Password:        os.Getenv("DB_PASSWORD"),
		ReplicationSlot: "effectus_slot",
		Publication:     "effectus_changes",
		TableMappings: map[string]*adapters.TableMapping{
			"users": {
				FactType:      "acme.v1.facts.UserProfile",
				SchemaVersion: "v1.0.0",
				KeyFields:     []string{"id"},
				ChangeTypes:   []string{"INSERT", "UPDATE", "DELETE"},
				FieldMappings: map[string]string{
					"id":    "user_id",
					"email": "email",
					"name":  "name",
				},
			},
			"orders": {
				FactType:      "acme.v1.facts.OrderEvent",
				SchemaVersion: "v1.0.0",
				KeyFields:     []string{"order_id"},
				ChangeTypes:   []string{"INSERT", "UPDATE"},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create Postgres source: %w", err)
	}

	if err := system.RegisterFactSource("postgres_changes", postgresSource); err != nil {
		return fmt.Errorf("failed to register Postgres source: %w", err)
	}

	log.Println("✅ Registered fact sources:")
	log.Println("  📨 Kafka (user.events)")
	log.Println("  🔗 HTTP Webhooks (:8081/webhook/external-events)")
	log.Println("  🗄️  PostgreSQL CDC (users, orders tables)")

	return nil
}

func registerVerbExecutors(system *unified.DynamicSystem) error {
	// Register email service
	emailExecutor := &EmailVerbExecutor{
		smtpHost: "smtp.example.com",
		smtpPort: 587,
		username: os.Getenv("SMTP_USERNAME"),
		password: os.Getenv("SMTP_PASSWORD"),
	}
	system.RegisterVerbExecutor("send_email", emailExecutor)

	// Register HTTP client
	httpExecutor := &HTTPVerbExecutor{
		client:  &http.Client{Timeout: 30 * time.Second},
		timeout: 30 * time.Second,
	}
	system.RegisterVerbExecutor("http_request", httpExecutor)

	// Register notification service
	notificationExecutor := &NotificationVerbExecutor{
		pushService: NewPushNotificationService(),
		smsService:  NewSMSService(),
	}
	system.RegisterVerbExecutor("send_notification", notificationExecutor)

	log.Println("✅ Registered verb executors:")
	log.Println("  📧 Email Service")
	log.Println("  🌐 HTTP Client")
	log.Println("  📱 Notification Service")

	return nil
}

func loadInitialRules(system *unified.DynamicSystem) error {
	// Example rules that work with the multi-source facts
	rules := []*unified.RuleDefinition{
		{
			Name: "welcome_new_users",
			Type: unified.RuleTypeList,
			Predicates: []*unified.PredicateDefinition{
				{
					FactType:      "acme.v1.facts.UserProfile",
					SchemaVersion: "v1.0.0",
					Path:          "status",
					Operator:      "equals",
					Value:         "ACCOUNT_STATUS_ACTIVE",
				},
				{
					FactType:      "acme.v1.facts.UserProfile",
					SchemaVersion: "v1.0.0",
					Path:          "created_at",
					Operator:      "within",
					Value:         "24h",
				},
			},
			Effects: []*unified.EffectDefinition{
				{
					VerbName:         "send_email",
					InterfaceVersion: "v1.0.0",
					Args: map[string]interface{}{
						"to":       "{{.UserProfile.Email}}",
						"subject":  "Welcome to our platform!",
						"template": "welcome_email",
						"data":     "{{.UserProfile}}",
					},
				},
				{
					VerbName:         "send_notification",
					InterfaceVersion: "v1.0.0",
					Args: map[string]interface{}{
						"user_id": "{{.UserProfile.UserId}}",
						"type":    "welcome",
						"title":   "Welcome!",
						"message": "Your account is now active",
					},
				},
			},
		},
		{
			Name: "alert_suspicious_activity",
			Type: unified.RuleTypeList,
			Predicates: []*unified.PredicateDefinition{
				{
					FactType:      "acme.v1.facts.UserSession",
					SchemaVersion: "v1.0.0",
					Path:          "login_attempts",
					Operator:      "greater_than",
					Value:         "5",
				},
				{
					FactType:      "acme.v1.facts.UserSession",
					SchemaVersion: "v1.0.0",
					Path:          "time_window",
					Operator:      "within",
					Value:         "10m",
				},
			},
			Effects: []*unified.EffectDefinition{
				{
					VerbName:         "http_request",
					InterfaceVersion: "v1.0.0",
					Args: map[string]interface{}{
						"url":    "https://security-api.example.com/alerts",
						"method": "POST",
						"body": map[string]interface{}{
							"alert_type": "suspicious_login",
							"user_id":    "{{.UserSession.UserId}}",
							"ip_address": "{{.UserSession.IpAddress}}",
							"attempts":   "{{.UserSession.LoginAttempts}}",
						},
					},
				},
			},
		},
		{
			Name: "process_order_updates",
			Type: unified.RuleTypeFlow,
			Predicates: []*unified.PredicateDefinition{
				{
					FactType:      "acme.v1.facts.OrderEvent",
					SchemaVersion: "v1.0.0",
					Path:          "status",
					Operator:      "equals",
					Value:         "COMPLETED",
				},
			},
			Effects: []*unified.EffectDefinition{
				{
					VerbName:         "send_email",
					InterfaceVersion: "v1.0.0",
					Args: map[string]interface{}{
						"to":       "{{.OrderEvent.CustomerEmail}}",
						"subject":  "Order Completed: {{.OrderEvent.OrderId}}",
						"template": "order_completion",
						"data":     "{{.OrderEvent}}",
					},
				},
				{
					VerbName:         "http_request",
					InterfaceVersion: "v1.0.0",
					Args: map[string]interface{}{
						"url":    "https://analytics.example.com/events",
						"method": "POST",
						"body": map[string]interface{}{
							"event_type": "order_completed",
							"order_id":   "{{.OrderEvent.OrderId}}",
							"value":      "{{.OrderEvent.TotalAmount}}",
							"timestamp":  "{{.OrderEvent.CompletedAt}}",
						},
					},
				},
			},
		},
	}

	// Compile and load rules
	for _, rule := range rules {
		if err := system.LoadRule(rule); err != nil {
			return fmt.Errorf("failed to load rule %s: %w", rule.Name, err)
		}
	}

	log.Println("✅ Loaded initial rules:")
	log.Println("  👋 welcome_new_users (List rule)")
	log.Println("  🚨 alert_suspicious_activity (List rule)")
	log.Println("  📦 process_order_updates (Flow rule)")

	return nil
}

// Example verb executor implementations
type EmailVerbExecutor struct {
	smtpHost string
	smtpPort int
	username string
	password string
}

func (e *EmailVerbExecutor) Execute(ctx context.Context, effect *unified.TypedEffect) (*unified.VerbResult, error) {
	// Implementation for sending emails
	log.Printf("📧 Sending email: %+v", effect.Args)

	// Simulate email sending
	time.Sleep(100 * time.Millisecond)

	return &unified.VerbResult{
		Success: true,
		Output: map[string]interface{}{
			"message_id": fmt.Sprintf("msg_%d", time.Now().Unix()),
			"status":     "sent",
		},
	}, nil
}

type HTTPVerbExecutor struct {
	client  *http.Client
	timeout time.Duration
}

func (h *HTTPVerbExecutor) Execute(ctx context.Context, effect *unified.TypedEffect) (*unified.VerbResult, error) {
	// Implementation for HTTP requests
	log.Printf("🌐 Making HTTP request: %+v", effect.Args)

	// Simulate HTTP request
	time.Sleep(200 * time.Millisecond)

	return &unified.VerbResult{
		Success: true,
		Output: map[string]interface{}{
			"status_code": 200,
			"response":    "OK",
		},
	}, nil
}

type NotificationVerbExecutor struct {
	pushService PushNotificationService
	smsService  SMSService
}

func (n *NotificationVerbExecutor) Execute(ctx context.Context, effect *unified.TypedEffect) (*unified.VerbResult, error) {
	// Implementation for notifications
	log.Printf("📱 Sending notification: %+v", effect.Args)

	// Simulate notification sending
	time.Sleep(50 * time.Millisecond)

	return &unified.VerbResult{
		Success: true,
		Output: map[string]interface{}{
			"notification_id": fmt.Sprintf("notif_%d", time.Now().Unix()),
			"delivered":       true,
		},
	}, nil
}

// Placeholder interfaces
type PushNotificationService interface{}
type SMSService interface{}

func NewPushNotificationService() PushNotificationService { return nil }
func NewSMSService() SMSService                           { return nil }
