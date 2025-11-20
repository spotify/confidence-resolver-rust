package main

import (
	"context"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence"
)

func main() {
	ctx := context.Background()

	// Load configuration from environment variables
	apiClientID := getEnvOrDefault("CONFIDENCE_API_CLIENT_ID", "API_ID")
	apiClientSecret := getEnvOrDefault("CONFIDENCE_API_CLIENT_SECRET", "API_SECRET")
	clientSecret := getEnvOrDefault("CONFIDENCE_CLIENT_SECRET", "CLIENT_SECRET")

	// Validate configuration - fail fast on placeholder credentials
	if apiClientID == "API_ID" || apiClientSecret == "API_SECRET" || clientSecret == "CLIENT_SECRET" {
		log.Fatalf("ERROR: Placeholder credentials detected. Please set environment variables:\n" +
			"  - CONFIDENCE_API_CLIENT_ID\n" +
			"  - CONFIDENCE_API_CLIENT_SECRET\n" +
			"  - CONFIDENCE_CLIENT_SECRET\n\n" +
			"Example:\n" +
			"  export CONFIDENCE_API_CLIENT_ID=\"your-api-client-id\"\n" +
			"  export CONFIDENCE_API_CLIENT_SECRET=\"your-api-client-secret\"\n" +
			"  export CONFIDENCE_CLIENT_SECRET=\"your-client-secret\"\n")
	}

	log.Println("Starting Confidence OpenFeature Local Provider Demo")
	log.Println("")

	// Create provider with simple configuration
	log.Println("Creating Confidence provider...")

	provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
		APIClientID:     apiClientID,
		APIClientSecret: apiClientSecret,
		ClientSecret:    clientSecret,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	defer openfeature.Shutdown()
	log.Println("Confidence provider created successfully")

	// Register with OpenFeature
	err = openfeature.SetProviderAndWait(provider)
	if err != nil {
		return
	}
	log.Println("OpenFeature provider registered")
	log.Println("")

	// Create OpenFeature client
	client := openfeature.NewClient("demo-app")

	// Demo: Evaluate flags continuously at 5 resolves per second
	log.Println("=== Flag Evaluation Demo - 5 Resolves/Second (Continuous) ===")
	log.Println("")

	// Create evaluation context
	evalCtx := openfeature.NewEvaluationContext(
		"user-123",
		map[string]interface{}{
			"user_id":    "vahid",
			"visitor_id": "vahid",
		},
	)

	log.Println("Starting continuous evaluation at 5 resolves/second...")
	log.Println("Press Ctrl+C to stop")
	log.Println("")

	startTime := time.Now()

	// Shared counters for throughput calculation
	var totalSuccess, totalErrors int64

	// Create ticker for 5 resolves per second (200ms interval)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	// Stats reporting ticker (every 10 seconds)
	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	iteration := 0
	for {
		select {
		case <-ticker.C:
			// Use ObjectValueDetails to get the full flag object
			result, err := client.ObjectValueDetails(ctx, "mattias-boolean-flag", map[string]interface{}{}, evalCtx)
			if err != nil {
				atomic.AddInt64(&totalErrors, 1)
				if iteration == 0 { // Only log first error
					log.Printf("Error: %v", err)
				}
			} else {
				atomic.AddInt64(&totalSuccess, 1)
				if iteration == 0 { // Only log first success
					log.Printf("First result - Value: %+v, Variant: %s, Reason: %s",
						result.Value, result.Variant, result.Reason)
				}
			}
			iteration++

		case <-statsTicker.C:
			duration := time.Since(startTime)
			totalRequests := atomic.LoadInt64(&totalSuccess) + atomic.LoadInt64(&totalErrors)
			throughputPerSecond := float64(totalRequests) / duration.Seconds()

			log.Printf("Stats: %d total requests, %.2f req/s, %d successes, %d errors",
				totalRequests, throughputPerSecond,
				atomic.LoadInt64(&totalSuccess), atomic.LoadInt64(&totalErrors))
		}
	}
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func printBooleanResult(result openfeature.BooleanEvaluationDetails) {
	log.Printf("  Value: %v", result.Value)
	log.Printf("  Variant: %s", result.Variant)
	log.Printf("  Reason: %s", result.Reason)
	if result.ErrorCode != "" {
		log.Printf("  Error Code: %s", result.ErrorCode)
		log.Printf("  Error Message: %s", result.ErrorMessage)
	}
}

func printStringResult(result openfeature.StringEvaluationDetails) {
	log.Printf("  Value: %s", result.Value)
	log.Printf("  Variant: %s", result.Variant)
	log.Printf("  Reason: %s", result.Reason)
	if result.ErrorCode != "" {
		log.Printf("  Error Code: %s", result.ErrorCode)
		log.Printf("  Error Message: %s", result.ErrorMessage)
	}
}

func printIntResult(result openfeature.IntEvaluationDetails) {
	log.Printf("  Value: %d", result.Value)
	log.Printf("  Variant: %s", result.Variant)
	log.Printf("  Reason: %s", result.Reason)
	if result.ErrorCode != "" {
		log.Printf("  Error Code: %s", result.ErrorCode)
		log.Printf("  Error Message: %s", result.ErrorMessage)
	}
}

func printFloatResult(result openfeature.FloatEvaluationDetails) {
	log.Printf("  Value: %f", result.Value)
	log.Printf("  Variant: %s", result.Variant)
	log.Printf("  Reason: %s", result.Reason)
	if result.ErrorCode != "" {
		log.Printf("  Error Code: %s", result.ErrorCode)
		log.Printf("  Error Message: %s", result.ErrorMessage)
	}
}

func printObjectResult(result openfeature.InterfaceEvaluationDetails) {
	log.Printf("  Value: %+v", result.Value)
	log.Printf("  Variant: %s", result.Variant)
	log.Printf("  Reason: %s", result.Reason)
	if result.ErrorCode != "" {
		log.Printf("  Error Code: %s", result.ErrorCode)
		log.Printf("  Error Message: %s", result.ErrorMessage)
	}
}
