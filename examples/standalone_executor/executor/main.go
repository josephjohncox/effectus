package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/josephjohncox/effectus/executorhttp"
	_ "github.com/lib/pq"
)

type service struct {
	database *sql.DB
}

type review struct {
	ReviewID       string    `json:"review_id"`
	OrderID        string    `json:"order_id"`
	Reason         string    `json:"reason"`
	Status         string    `json:"status"`
	ExecutionID    string    `json:"execution_id"`
	EffectID       string    `json:"effect_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	ArgumentHash   string    `json:"argument_hash"`
	CreatedAt      time.Time `json:"created_at"`
	Replay         bool      `json:"replay"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("BUSINESS_POSTGRES_DSN"))
	if dsn == "" {
		return fmt.Errorf("BUSINESS_POSTGRES_DSN is required")
	}
	token := strings.TrimSpace(os.Getenv("EXECUTOR_TOKEN"))
	if token == "" {
		return fmt.Errorf("EXECUTOR_TOKEN is required")
	}
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open business database: %w", err)
	}
	defer database.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := waitForDatabase(ctx, database); err != nil {
		return err
	}
	if err := migrate(ctx, database); err != nil {
		return err
	}

	business := &service{database: database}
	reviews, err := executorhttp.NewHandler(executorhttp.Options{}, business.requestReview)
	if err != nil {
		return err
	}
	cancellations, err := executorhttp.NewHandler(executorhttp.Options{}, business.cancelReview)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("POST /reviews", requireToken(token, reviews))
	mux.Handle("POST /reviews/cancel", requireToken(token, cancellations))
	mux.Handle("GET /reviews", requireToken(token, http.HandlerFunc(business.listReviews)))
	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write([]byte("ok\n"))
	})
	server := &http.Server{
		Addr:              ":8090",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errChannel := make(chan error, 1)
	go func() {
		errChannel <- server.ListenAndServe()
	}()
	log.Printf("business executor listening on %s", server.Addr)
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-errChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func (service *service) requestReview(ctx context.Context, request executorhttp.Request) executorhttp.Outcome {
	if request.Metadata.Saga.Direction != executorhttp.DirectionForward {
		return executorhttp.Permanent(fmt.Errorf("RequestManualReview does not support compensation"))
	}
	orderID, ok := request.Arguments["orderId"].(string)
	if !ok || strings.TrimSpace(orderID) == "" {
		return executorhttp.Permanent(fmt.Errorf("orderId must be a non-empty string"))
	}
	reason, ok := request.Arguments["reason"].(string)
	if !ok || strings.TrimSpace(reason) == "" {
		return executorhttp.Permanent(fmt.Errorf("reason must be a non-empty string"))
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return executorhttp.Retryable(fmt.Errorf("begin review transaction: %w", err))
	}
	defer transaction.Rollback()

	result := review{
		ReviewID:       "review-" + orderID,
		OrderID:        orderID,
		Reason:         reason,
		Status:         "pending",
		ExecutionID:    request.Metadata.ExecutionID,
		EffectID:       request.Metadata.Saga.EffectID,
		IdempotencyKey: request.Metadata.Saga.IdempotencyKey,
		ArgumentHash:   request.ArgumentHash,
	}
	err = transaction.QueryRowContext(ctx, `
INSERT INTO order_reviews (
  idempotency_key, argument_hash, review_id, order_id, reason, status,
  execution_id, effect_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (idempotency_key) DO NOTHING
RETURNING created_at`,
		result.IdempotencyKey, result.ArgumentHash, result.ReviewID, result.OrderID,
		result.Reason, result.Status, result.ExecutionID, result.EffectID,
	).Scan(&result.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		result.Replay = true
		err = transaction.QueryRowContext(ctx, `
SELECT argument_hash, review_id, order_id, reason, status, execution_id,
       effect_id, created_at
FROM order_reviews
WHERE idempotency_key = $1`, result.IdempotencyKey).Scan(
			&result.ArgumentHash, &result.ReviewID, &result.OrderID, &result.Reason,
			&result.Status, &result.ExecutionID, &result.EffectID, &result.CreatedAt,
		)
		if err != nil {
			return executorhttp.Unknown(fmt.Errorf("read existing review: %w", err))
		}
		if result.ArgumentHash != request.ArgumentHash {
			return executorhttp.Permanent(fmt.Errorf("idempotency key is already bound to different arguments"))
		}
	} else if err != nil {
		return executorhttp.Unknown(fmt.Errorf("insert review: %w", err))
	}
	if err := transaction.Commit(); err != nil {
		return executorhttp.Unknown(fmt.Errorf("commit review transaction: %w", err))
	}
	return executorhttp.Success(result.ReviewID)
}

func (service *service) cancelReview(ctx context.Context, request executorhttp.Request) executorhttp.Outcome {
	if request.Metadata.Saga.Direction != executorhttp.DirectionCompensation {
		return executorhttp.Permanent(fmt.Errorf("CancelManualReview requires compensation direction"))
	}
	orderID, ok := request.Arguments["orderId"].(string)
	if !ok || strings.TrimSpace(orderID) == "" {
		return executorhttp.Permanent(fmt.Errorf("orderId must be a non-empty string"))
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return executorhttp.Retryable(fmt.Errorf("begin cancellation transaction: %w", err))
	}
	defer transaction.Rollback()
	var reviewID, cancelKey, cancelHash string
	err = transaction.QueryRowContext(ctx, `
SELECT review_id, COALESCE(cancel_idempotency_key, ''),
       COALESCE(cancel_argument_hash, '')
FROM order_reviews
WHERE order_id = $1
FOR UPDATE`, orderID).Scan(&reviewID, &cancelKey, &cancelHash)
	if errors.Is(err, sql.ErrNoRows) {
		return executorhttp.Permanent(fmt.Errorf("review for order %q does not exist", orderID))
	}
	if err != nil {
		return executorhttp.Unknown(fmt.Errorf("lock review for cancellation: %w", err))
	}
	if cancelKey != "" {
		if cancelKey != request.Metadata.Saga.IdempotencyKey || cancelHash != request.ArgumentHash {
			return executorhttp.Permanent(fmt.Errorf("review cancellation identity conflicts with the stored operation"))
		}
		if err := transaction.Commit(); err != nil {
			return executorhttp.Unknown(fmt.Errorf("commit cancellation replay: %w", err))
		}
		return executorhttp.Success(reviewID)
	}
	if _, err := transaction.ExecContext(ctx, `
UPDATE order_reviews
SET status = 'cancelled', cancel_idempotency_key = $1,
    cancel_argument_hash = $2
WHERE order_id = $3`, request.Metadata.Saga.IdempotencyKey, request.ArgumentHash, orderID); err != nil {
		return executorhttp.Unknown(fmt.Errorf("cancel review: %w", err))
	}
	if err := transaction.Commit(); err != nil {
		return executorhttp.Unknown(fmt.Errorf("commit review cancellation: %w", err))
	}
	return executorhttp.Success(reviewID)
}

func (service *service) listReviews(response http.ResponseWriter, request *http.Request) {
	rows, err := service.database.QueryContext(request.Context(), `
SELECT review_id, order_id, reason, status, execution_id, effect_id,
       idempotency_key, argument_hash, created_at
FROM order_reviews
ORDER BY created_at, review_id`)
	if err != nil {
		http.Error(response, "query reviews", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	reviews := make([]review, 0)
	for rows.Next() {
		var item review
		if err := rows.Scan(
			&item.ReviewID, &item.OrderID, &item.Reason, &item.Status,
			&item.ExecutionID, &item.EffectID, &item.IdempotencyKey,
			&item.ArgumentHash, &item.CreatedAt,
		); err != nil {
			http.Error(response, "scan reviews", http.StatusInternalServerError)
			return
		}
		reviews = append(reviews, item)
	}
	if err := rows.Err(); err != nil {
		http.Error(response, "read reviews", http.StatusInternalServerError)
		return
	}
	response.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(response).Encode(map[string]any{"reviews": reviews})
}

func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Demo-Token") != token {
			http.Error(response, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func waitForDatabase(ctx context.Context, database *sql.DB) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := database.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("business database did not become ready")
		case <-ticker.C:
		}
	}
}

func migrate(ctx context.Context, database *sql.DB) error {
	_, err := database.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS order_reviews (
  idempotency_key TEXT PRIMARY KEY,
  argument_hash TEXT NOT NULL,
  review_id TEXT NOT NULL UNIQUE,
  order_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  status TEXT NOT NULL,
  execution_id TEXT NOT NULL,
  effect_id TEXT NOT NULL,
  cancel_idempotency_key TEXT,
  cancel_argument_hash TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`)
	if err != nil {
		return fmt.Errorf("migrate business database: %w", err)
	}
	return nil
}
