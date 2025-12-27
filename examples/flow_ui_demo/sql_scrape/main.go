package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/effectus/effectus-go/adapters"
	_ "github.com/effectus/effectus-go/adapters/sql"
)

const (
	defaultDSN         = "postgres://effectus:effectus@localhost:55432/effectus_ui_demo?sslmode=disable"
	defaultEffectusURL = "http://localhost:8080"
	defaultPoll        = 15 * time.Second
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	dsn := envOrDefault("SQL_SCRAPE_DSN", defaultDSN)
	poll := parseDuration(envOrDefault("SQL_SCRAPE_POLL", defaultPoll.String()), defaultPoll)
	effectusURL := strings.TrimRight(envOrDefault("EFFECTUS_URL", defaultEffectusURL), "/")
	token := envOrDefault("EFFECTUS_TOKEN", "")

	config := adapters.SourceConfig{
		SourceID: "flow_ui_sql_scrape",
		Type:     "sql",
		Config: map[string]interface{}{
			"driver":         "postgres",
			"dsn":            dsn,
			"mode":           "batch",
			"query":          "SELECT order_id, amount, currency, state, created_at_epoch, priority, sku, customer_id, customer_risk_score, customer_priority, payment_auth_id, payment_state, inventory_available, device_id, risk_stream_score, chargeback_open, updated_at_epoch FROM scrape_orders ORDER BY updated_at_epoch DESC LIMIT 1",
			"poll_interval":  poll.String(),
			"schema_name":    "flow.ui.demo.sql_row",
			"schema_version": "v1",
		},
	}

	source, err := adapters.CreateSource(config)
	if err != nil {
		log.Fatalf("create sql source: %v", err)
	}
	if err := source.Start(ctx); err != nil {
		log.Fatalf("start sql source: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = source.Stop(stopCtx)
	}()

	factChan, err := source.Subscribe(ctx, nil)
	if err != nil {
		log.Fatalf("subscribe sql source: %v", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	var lastUpdated int64

	log.Printf("SQL scrape demo polling %s every %s", dsn, poll)
	log.Printf("Forwarding facts to %s", effectusURL)

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping SQL scrape demo")
			return
		case fact, ok := <-factChan:
			if !ok {
				log.Println("Fact channel closed")
				return
			}
			row := fact.Data.AsMap()
			updated := toInt64(row["updated_at_epoch"])
			if updated > 0 && updated <= lastUpdated {
				continue
			}
			if updated > 0 {
				lastUpdated = updated
			}

			facts := mapRowToFacts(row)
			if len(facts) == 0 {
				continue
			}
			if err := postFacts(ctx, client, effectusURL, token, facts); err != nil {
				log.Printf("post facts error: %v", err)
			} else {
				log.Printf("ingested SQL scrape facts (updated_at_epoch=%d)", updated)
			}
		}
	}
}

func mapRowToFacts(row map[string]interface{}) map[string]interface{} {
	orderID := toString(row["order_id"])
	if orderID == "" {
		return nil
	}
	facts := map[string]interface{}{
		"order": map[string]interface{}{
			"id":               orderID,
			"amount":           toFloat(row["amount"]),
			"currency":         toString(row["currency"]),
			"state":            toString(row["state"]),
			"created_at_epoch": toInt64(row["created_at_epoch"]),
			"priority":         toString(row["priority"]),
			"sku":              toString(row["sku"]),
		},
		"customer": map[string]interface{}{
			"id":         toString(row["customer_id"]),
			"risk_score": toInt64(row["customer_risk_score"]),
			"priority":   toInt64(row["customer_priority"]),
		},
		"payment": map[string]interface{}{
			"authorization_id": toString(row["payment_auth_id"]),
			"state":            toString(row["payment_state"]),
		},
		"inventory": map[string]interface{}{
			"available": toBool(row["inventory_available"]),
		},
		"device": map[string]interface{}{
			"id": toString(row["device_id"]),
		},
		"risk": map[string]interface{}{
			"stream_score": toInt64(row["risk_stream_score"]),
		},
		"chargeback": map[string]interface{}{
			"open": toBool(row["chargeback_open"]),
		},
		"clock": map[string]interface{}{
			"current_epoch": currentEpoch(row["updated_at_epoch"]),
		},
	}
	return facts
}

func currentEpoch(updated interface{}) int64 {
	if value := toInt64(updated); value > 0 {
		return value
	}
	return time.Now().Unix()
}

func postFacts(ctx context.Context, client *http.Client, baseURL, token string, facts map[string]interface{}) error {
	payload := map[string]interface{}{
		"universe": "default",
		"facts":    facts,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/facts", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post facts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("unexpected status %s", resp.Status)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseDuration(raw string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return parsed
}

func toString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case fmt.Stringer:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func toInt64(value interface{}) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return parsed
		}
	case []byte:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func toFloat(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return parsed
		}
	case []byte:
		if parsed, err := strconv.ParseFloat(strings.TrimSpace(string(v)), 64); err == nil {
			return parsed
		}
	}
	return 0
}

func toBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case []byte:
		parsed, err := strconv.ParseBool(strings.TrimSpace(string(v)))
		if err == nil {
			return parsed
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}
