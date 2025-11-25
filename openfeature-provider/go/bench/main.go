package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	openfeature "github.com/open-feature/go-sdk/openfeature"
	"github.com/spotify/confidence-resolver/openfeature-provider/go/confidence"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type stats struct {
	completed uint64
	errors    uint64
}

func main() {
	var (
		mockGRPCAddr    string
		durationSeconds int
		warmupSeconds   int
		threads         int
		gomaxprocs      int
		flagKey         string
		clientSecret    string
		pollInterval    int
	)

	flag.StringVar(&mockGRPCAddr, "mock-grpc", "localhost:8081", "mock support server gRPC address host:port")
	flag.IntVar(&durationSeconds, "duration", 30, "benchmark duration in seconds (excludes warmup)")
	flag.IntVar(&warmupSeconds, "warmup", 5, "warmup duration in seconds before measurement")
	flag.IntVar(&threads, "threads", runtime.NumCPU(), "number of concurrent worker goroutines")
	flag.IntVar(&gomaxprocs, "gomaxprocs", 0, "set GOMAXPROCS (0=leave default)")
	flag.StringVar(&flagKey, "flag", "example-flag", "flag key (without 'flags/' prefix)")
	flag.StringVar(&clientSecret, "client-secret", "secret", "client secret for request signing")
	flag.IntVar(&pollInterval, "poll-interval", 10, "resolver state/log poll interval in seconds (env override)")
	flag.Parse()

	if gomaxprocs > 0 {
		runtime.GOMAXPROCS(gomaxprocs)
	}
	if threads < 1 {
		threads = 1
	}
	if warmupSeconds < 0 {
		warmupSeconds = 0
	}
	if durationSeconds < 1 {
		durationSeconds = 1
	}

	ctx := context.Background()

	connFactory := func(ctx context.Context, _ string, defaultOpts []grpc.DialOption) (grpc.ClientConnInterface, error) {
		opts := append([]grpc.DialOption{}, defaultOpts...)
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		return grpc.NewClient(mockGRPCAddr, opts...)
	}

	provider, err := confidence.NewProvider(ctx, confidence.ProviderConfig{
		ClientSecret: clientSecret,
		ConnFactory:  connFactory,
	})
	provider.Init(openfeature.NewTargetlessEvaluationContext(map[string]any{}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create provider: %v\n", err)
		os.Exit(1)
	}

	// Minimal evaluation context; you can extend with attributes to exercise targeting
	evalCtx := openfeature.FlattenedContext{"targetingKey": "tutorial_visitor", "visitor_id": "tutorial_visitor"}

	// Prepare cancellation on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Warmup (abort on first error)
	if warmupSeconds > 0 {
		warmupCtx, cancel := context.WithTimeout(ctx, time.Duration(warmupSeconds)*time.Second)
		var warm stats
		runWorkers(warmupCtx, provider, flagKey, evalCtx, threads, &warm, cancel, true)
		cancel()
		if atomic.LoadUint64(&warm.errors) > 0 {
			fmt.Fprintf(os.Stderr, "aborting: error during warmup\n")
			os.Exit(1)
		}
	}

	// Measurement
	measureCtx, cancelMeasure := context.WithTimeout(ctx, time.Duration(durationSeconds)*time.Second)
	defer cancelMeasure()

	var s stats
	// Abort early on signal
	go func() {
		select {
		case <-sigCh:
			cancelMeasure()
		case <-measureCtx.Done():
		}
	}()

	start := time.Now()
	runWorkers(measureCtx, provider, flagKey, evalCtx, threads, &s, cancelMeasure, true)
	elapsed := time.Since(start)
	provider.Shutdown()

	completed := atomic.LoadUint64(&s.completed)
	errs := atomic.LoadUint64(&s.errors)
	qps := float64(completed) / elapsed.Seconds()

	fmt.Printf("flag=%s threads=%d duration=%s ops=%d errors=%d throughput=%.0f ops/s\n",
		flagKey, threads, elapsed.Truncate(time.Millisecond), completed, errs, qps)
}

func runWorkers(ctx context.Context, provider *confidence.LocalResolverProvider, flagKey string, evalCtx openfeature.FlattenedContext, threads int, s *stats, cancel context.CancelFunc, abortOnError bool) {
	wg := sync.WaitGroup{}
	wg.Add(threads)
	for i := 0; i < threads; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					res := provider.ObjectEvaluation(context.Background(), flagKey, nil, evalCtx)
					if s != nil {
						atomic.AddUint64(&s.completed, 1)
						// fmt.Printf("reason %s", res.Reason)
						if res.Reason == openfeature.ErrorReason {
							atomic.AddUint64(&s.errors, 1)
							if abortOnError && cancel != nil {
								cancel()
								return
							}
						}
					}
				}
			}
		}()
	}
	wg.Wait()
}
