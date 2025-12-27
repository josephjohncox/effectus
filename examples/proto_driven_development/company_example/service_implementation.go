//go:build proto_demo
// +build proto_demo

package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	// Generated from our proto files
	verbsv1 "acme.com/gen/go/acme/v1/verbs"
	effectusv1 "github.com/effectus/effectus-go/gen/effectus/v1"
)

// NotificationService implements the send_notification verb
// This demonstrates how a company would build their service using proto-first development
type NotificationService struct {
	emailProvider EmailProvider
	smsProvider   SMSProvider
	pushProvider  PushProvider
}

// EmailProvider interface for sending emails
type EmailProvider interface {
	SendEmail(ctx context.Context, userID, message string, metadata map[string]string) (string, error)
}

// SMSProvider interface for sending SMS
type SMSProvider interface {
	SendSMS(ctx context.Context, userID, message string, metadata map[string]string) (string, error)
}

// PushProvider interface for sending push notifications
type PushProvider interface {
	SendPush(ctx context.Context, userID, message string, metadata map[string]string) (string, error)
}

// Execute implements the send_notification verb using fully typed protobuf messages
func (s *NotificationService) Execute(
	ctx context.Context,
	input *verbsv1.SendNotificationInput,
) (*verbsv1.SendNotificationOutput, error) {

	// Type-safe access to all input fields
	log.Printf("Sending %s notification to user %s: %s",
		input.Type.String(), input.UserId, input.Message)

	// Handle priority (with safe defaults)
	priority := input.Priority
	if priority == nil {
		defaultPriority := verbsv1.Priority_PRIORITY_NORMAL
		priority = &defaultPriority
	}

	// Route based on notification type with full type safety
	var providerID string
	var err error

	switch input.Type {
	case verbsv1.NotificationType_NOTIFICATION_TYPE_EMAIL:
		providerID, err = s.emailProvider.SendEmail(ctx, input.UserId, input.Message, input.Metadata)

	case verbsv1.NotificationType_NOTIFICATION_TYPE_SMS:
		providerID, err = s.smsProvider.SendSMS(ctx, input.UserId, input.Message, input.Metadata)

	case verbsv1.NotificationType_NOTIFICATION_TYPE_PUSH:
		providerID, err = s.pushProvider.SendPush(ctx, input.UserId, input.Message, input.Metadata)

	default:
		return nil, fmt.Errorf("unsupported notification type: %v", input.Type)
	}

	// Handle errors with proper typing
	if err != nil {
		errMsg := err.Error()
		return &verbsv1.SendNotificationOutput{
			NotificationId: generateNotificationID(),
			Status:         verbsv1.DeliveryStatus_DELIVERY_STATUS_FAILED,
			SentAt:         timestamppb.Now(),
			Success:        false,
			ErrorMessage:   &errMsg,
		}, nil // Return success with error details in response
	}

	// Success response with provider details
	providerDetails := &verbsv1.ProviderDetails{
		Provider:          getProviderName(input.Type),
		ProviderMessageId: &providerID,
		ProviderStatus:    stringPtr("accepted"),
		ProviderMetadata:  make(map[string]string),
	}

	// Add priority to provider metadata
	if priority != nil {
		providerDetails.ProviderMetadata["priority"] = priority.String()
	}

	return &verbsv1.SendNotificationOutput{
		NotificationId:    generateNotificationID(),
		Status:            verbsv1.DeliveryStatus_DELIVERY_STATUS_QUEUED,
		SentAt:            timestamppb.Now(),
		EstimatedDelivery: estimateDelivery(input.Type, *priority),
		ProviderDetails:   providerDetails,
		Success:           true,
	}, nil
}

// Integration with Effectus runtime - register the verb
func (s *NotificationService) RegisterWithEffectus(runtime *effectusv1.ExecutionRuntime) error {
	// Register this verb implementation with Effectus
	// The runtime will handle routing based on the proto schema
	verbSpec := &effectusv1.VerbSpec{
		Name:    "send_notification",
		Version: "1.0.0",
		// Proto schema is automatically derived from the protobuf definition
		InputSchema:   getProtoSchema[verbsv1.SendNotificationInput](),
		OutputSchema:  getProtoSchema[verbsv1.SendNotificationOutput](),
		Capabilities:  []string{"notification.send"},
		ExecutionType: effectusv1.ExecutionType_EXECUTION_TYPE_LOCAL,
	}

	return runtime.RegisterVerb(verbSpec, s.Execute)
}

// Real-world usage example showing how teams use this
func main() {
	// Initialize service with real providers
	service := &NotificationService{
		emailProvider: &SendGridProvider{apiKey: "your-sendgrid-key"},
		smsProvider:   &TwilioProvider{accountSID: "your-twilio-sid", authToken: "your-twilio-token"},
		pushProvider:  &FirebaseProvider{projectID: "your-firebase-project"},
	}

	// Example: Company onboarding rule that sends welcome notifications
	ctx := context.Background()

	// This is a type-safe protobuf message - no JSON parsing, no runtime errors
	welcomeNotification := &verbsv1.SendNotificationInput{
		UserId:  "user_12345",
		Message: "Welcome to Acme Corp! Your account is now active.",
		Type:    verbsv1.NotificationType_NOTIFICATION_TYPE_EMAIL,
		Priority: func() *verbsv1.Priority {
			p := verbsv1.Priority_PRIORITY_HIGH
			return &p
		}(),
		TemplateId: stringPtr("welcome_template_v2"),
		TemplateVars: map[string]string{
			"user_name":    "John Doe",
			"company_name": "Acme Corp",
			"support_url":  "https://acme.com/support",
		},
		Tags: []string{"onboarding", "welcome", "user_activation"},
		Metadata: map[string]string{
			"campaign_id": "onboarding_2024_q1",
			"source":      "user_registration",
		},
	}

	// Execute with full type safety
	result, err := service.Execute(ctx, welcomeNotification)
	if err != nil {
		log.Fatalf("Failed to send notification: %v", err)
	}

	// Type-safe access to results
	if result.Success {
		log.Printf("✅ Notification sent successfully!")
		log.Printf("   ID: %s", result.NotificationId)
		log.Printf("   Status: %s", result.Status.String())
		log.Printf("   Provider: %s", result.ProviderDetails.Provider)
		if result.EstimatedDelivery != nil {
			log.Printf("   Estimated delivery: %s", result.EstimatedDelivery.AsTime().Format(time.RFC3339))
		}
	} else {
		log.Printf("❌ Notification failed: %s", *result.ErrorMessage)
	}
}

// Helper functions

func generateNotificationID() string {
	return fmt.Sprintf("notif_%d", time.Now().UnixNano())
}

func getProviderName(notifType verbsv1.NotificationType) string {
	switch notifType {
	case verbsv1.NotificationType_NOTIFICATION_TYPE_EMAIL:
		return "sendgrid"
	case verbsv1.NotificationType_NOTIFICATION_TYPE_SMS:
		return "twilio"
	case verbsv1.NotificationType_NOTIFICATION_TYPE_PUSH:
		return "firebase"
	default:
		return "unknown"
	}
}

func estimateDelivery(notifType verbsv1.NotificationType, priority verbsv1.Priority) *timestamppb.Timestamp {
	var delay time.Duration

	switch priority {
	case verbsv1.Priority_PRIORITY_URGENT:
		delay = 30 * time.Second
	case verbsv1.Priority_PRIORITY_HIGH:
		delay = 2 * time.Minute
	case verbsv1.Priority_PRIORITY_NORMAL:
		delay = 5 * time.Minute
	case verbsv1.Priority_PRIORITY_LOW:
		delay = 15 * time.Minute
	default:
		delay = 5 * time.Minute
	}

	// Email typically faster than SMS
	if notifType == verbsv1.NotificationType_NOTIFICATION_TYPE_EMAIL {
		delay = delay / 2
	}

	return timestamppb.New(time.Now().Add(delay))
}

func stringPtr(s string) *string {
	return &s
}

// Mock implementations of providers for example
type SendGridProvider struct{ apiKey string }

func (p *SendGridProvider) SendEmail(ctx context.Context, userID, message string, metadata map[string]string) (string, error) {
	return "sg_message_123", nil
}

type TwilioProvider struct{ accountSID, authToken string }

func (p *TwilioProvider) SendSMS(ctx context.Context, userID, message string, metadata map[string]string) (string, error) {
	return "tw_message_456", nil
}

type FirebaseProvider struct{ projectID string }

func (p *FirebaseProvider) SendPush(ctx context.Context, userID, message string, metadata map[string]string) (string, error) {
	return "fcm_message_789", nil
}

// Generic function to get proto schema (this would be implemented by Effectus)
func getProtoSchema[T any]() map[string]interface{} {
	// This would use protobuf reflection to extract schema
	// Implementation details depend on Effectus runtime
	return make(map[string]interface{})
}
