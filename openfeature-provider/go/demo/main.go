package main

import (
	"context"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/open-feature/go-sdk/openfeature"
	"github.com/spotify/confidence-resolver-rust/openfeature-provider/go/confidence"
)

func main() {
	ctx := context.Background()

	// Load configuration from environment variables
	apiClientID := getEnvOrDefault("CONFIDENCE_API_CLIENT_ID", "API_ID")
	apiClientSecret := getEnvOrDefault("CONFIDENCE_API_CLIENT_SECRET", "API_SECRET")
	clientSecret := getEnvOrDefault("CONFIDENCE_CLIENT_SECRET", "CLIENT_SECRET")

	// Validate configuration
	if apiClientID == "YOUR_API_CLIENT_ID" || apiClientSecret == "YOUR_API_CLIENT_SECRET" || clientSecret == "YOUR_CLIENT_SECRET" {
		log.Println("WARNING: Using placeholder credentials. Please set environment variables:")
		log.Println("  - CONFIDENCE_API_CLIENT_ID")
		log.Println("  - CONFIDENCE_API_CLIENT_SECRET")
		log.Println("  - CONFIDENCE_CLIENT_SECRET")
		log.Println("")
		log.Println("Continuing with demo (will fail authentication)...")
		log.Println("")
	}

	log.Println("Starting Confidence OpenFeature Local Provider Demo")
	log.Println("")

	// Create provider with simple configuration
	log.Println("Creating Confidence provider...")

	provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
		APIClientID:     apiClientID,
		DisableLogging:  true,
		APIClientSecret: apiClientSecret,
		ClientSecret:    clientSecret,
	})
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Shutdown()
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

	// Demo: Evaluate flags with multiple concurrent threads
	log.Println("=== Flag Evaluation Demo with 10 Concurrent Threads ===")
	log.Println("")

	// Create evaluation context
	evalCtx := openfeature.NewEvaluationContext(
		"user-123",
		map[string]interface{}{
			"user_id":    "vahid",
			"visitor_id": "vahid",
		},
	)

	// Run 10 concurrent threads continuously for at least 20 seconds
	var wg sync.WaitGroup
	numThreads := 20
	runDuration := 20 * time.Second

	log.Printf("Starting %d threads to run for %v to test reload and flush...", numThreads, runDuration)
	log.Println("")

	startTime := time.Now()
	stopTime := startTime.Add(runDuration)

	// Shared counters for throughput calculation
	var totalSuccess, totalErrors int64

	for i := 0; i < numThreads; i++ {
		wg.Add(1)
		threadID := i
		go func() {
			defer wg.Done()

			var successCount, errorCount int64
			iteration := 0

			for time.Now().Before(stopTime) {
				// Use ObjectValueDetails to get the full flag object
				result, err := client.ObjectValueDetails(ctx, "mattias-boolean-flag", map[string]interface{}{}, evalCtx)
				if err != nil {
					errorCount++
					if iteration == 0 { // Only log first error per thread
						log.Printf("Thread %d: Error: %v", threadID, err)
					}
				} else {
					successCount++
					if iteration == 0 { // Only log first success per thread
						log.Printf("Thread %d: First result - Value: %+v, Variant: %s, Reason: %s",
							threadID, result.Value, result.Variant, result.Reason)
					}
				}
				iteration++

				// Small sleep to avoid tight loop
				time.Sleep(1 * time.Millisecond)
			}

			// Update shared counters atomically
			atomic.AddInt64(&totalSuccess, successCount)
			atomic.AddInt64(&totalErrors, errorCount)

			log.Printf("Thread %d complete after %v: %d successes, %d errors (%d total iterations)",
				threadID, time.Since(startTime), successCount, errorCount, iteration)
		}()
	}

	// Wait for all threads to complete
	wg.Wait()

	duration := time.Since(startTime)
	totalRequests := totalSuccess + totalErrors
	throughputPerSecond := float64(totalRequests) / duration.Seconds()

	log.Println("")
	log.Println("=== Demo Complete ===")
	log.Printf("Total time: %v", duration)
	log.Printf("Throughput: %.2f requests/second", throughputPerSecond)
	log.Printf("Average latency: %.2f ms/request", duration.Seconds()*1000/float64(totalRequests))
	log.Println("Check logs above for per-thread statistics and state reload/flush messages")
	log.Println("")
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
