package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spotify/confidence-resolver-rust/mock-support-server/genproto/mock"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

type config struct {
	Port              int
	AccountID         string
	ResolverStatePath string
	// used to mock the correct state url
	ClientSecret    string
	RequestLogging    bool
	// Artificial per-request latency in milliseconds for both HTTP and gRPC
	LatencyMs int
	// Bandwidth cap for HTTP responses in kilobytes per second (0 disables throttling)
	BandwidthKbps int
}

func readEnv() config {
	cfg := config{
		Port:              getenvInt("PORT", 8081),
		AccountID:         getenv("ACCOUNT_ID", "confidence-test"),
		ResolverStatePath: getenv("RESOLVER_STATE_PB", ""),
		ClientSecret:    	 getenv("CLIENT_SECRET", "secret"),
		RequestLogging:    getenvBool("REQUEST_LOGGING", false),
		LatencyMs:         getenvInt("LATENCY_MS", 0),
		BandwidthKbps:     getenvInt("BANDWIDTH_KBPS", 0),
	}
	return cfg
}

type internalFlagLoggerService struct {
	pb.UnimplementedInternalFlagLoggerServiceServer
	clientSecret string
	bytesIn      atomic.Int64
	appliedCount atomic.Int64
	requestCount atomic.Int64
}

func (s *internalFlagLoggerService) ClientWriteFlagLogs(ctx context.Context, req *pb.WriteFlagLogsRequest) (*pb.WriteFlagLogsResponse, error) {
	// Require Authorization: "ClientSecret <secret>"
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		authVals := md.Get("authorization")
		if len(authVals) == 0 || authVals[0] != "ClientSecret "+s.clientSecret {
			return nil, status.Error(codes.Unauthenticated, "missing or invalid authorization")
		}
	} else {
		return nil, status.Error(codes.Unauthenticated, "missing authorization")
	}
	s.bytesIn.Add(int64(proto.Size(req)))
	s.appliedCount.Add(int64(len(req.FlagAssigned)))
	s.requestCount.Add(1)
	return &pb.WriteFlagLogsResponse{}, nil
}

func main() {
	cfg := readEnv()
	var grpcServer *grpc.Server
	{
		var unaryInterceptors []grpc.UnaryServerInterceptor
		if cfg.RequestLogging {
			unaryInterceptors = append(unaryInterceptors, unaryLoggingInterceptor)
		}
		if len(unaryInterceptors) > 0 {
			grpcServer = grpc.NewServer(
				grpc.ChainUnaryInterceptor(unaryInterceptors...),
			)
		} else {
			grpcServer = grpc.NewServer()
		}
	}

	// Shared implementation for both gRPC and HTTP (grpc-gateway)

	internalFlagLoggerServiceImpl := &internalFlagLoggerService{
		clientSecret: cfg.ClientSecret,
	}
	pb.RegisterInternalFlagLoggerServiceServer(grpcServer, internalFlagLoggerServiceImpl)

	// Periodic metrics log (once per second) for the lifetime of the server
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			b := internalFlagLoggerServiceImpl.bytesIn.Load()
			a := internalFlagLoggerServiceImpl.appliedCount.Load()
			r := internalFlagLoggerServiceImpl.requestCount.Load()
			log.Printf("metrics bytes_total=%d applied_total=%d req_total=%d", b, a, r)
		}
	}()

	// Build grpc-gateway and REST muxes
	ctx := context.Background()
	gw := runtime.NewServeMux(
		// Accept protobuf payloads for endpoints like /v1/flagLogs:write
		runtime.WithMarshalerOption("application/x-protobuf", &runtime.ProtoMarshaller{}),
	)
	if err := pb.RegisterInternalFlagLoggerServiceHandlerServer(ctx, gw, internalFlagLoggerServiceImpl); err != nil {
		log.Fatalf("failed to register grpc-gateway handlers: %v", err)
	}

	// Cdn server mock
	cdn := http.NewServeMux()
	// Serve state at path /<sha256hex of cfg.ClientSecret>
	stateHash := fmt.Sprintf("%x", sha256.Sum256([]byte(cfg.ClientSecret)))

	var stateBytes []byte
	if cfg.ResolverStatePath == "" {
		stateBytes = readStateFromUrl(stateHash)
	} else {
		stateBytes = readStateFromDisk(cfg.ResolverStatePath, cfg.AccountID)
	}
	var stateETag string // random ETag set on first successful response
	cdn.HandleFunc("/"+stateHash, func(w http.ResponseWriter, r *http.Request) {
		if len(stateBytes) == 0 {
			http.Error(w, "resolver state not configured", http.StatusNotFound)
			return
		}
		// Return 304 if client's ETag matches our current one
		if stateETag != "" && r.Header.Get("If-None-Match") == stateETag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		// Initialize random ETag on first response
		if stateETag == "" {
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err == nil {
				stateETag = fmt.Sprintf("\"%x\"", buf)
			} else {
				stateETag = fmt.Sprintf("\"%x-%x\"", time.Now().UnixNano(), len(stateBytes))
			}
		}
		w.Header().Set("ETag", stateETag)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(stateBytes)))
		if _, err := w.Write(stateBytes); err != nil {
			log.Printf("/state write error: %v", err)
		}
	})

	// Gateway mux serves resolver HTTP JSON/gRPC-gateway endpoints (mounted directly)

	// Unified handler that routes gRPC (h2c) vs REST
	base := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded := r.Header.Get("x-forwarded-host")
		if forwarded == "" {
			// using authority which is the common way to set forwarding for gRPC
			forwarded = r.Host
		}
		isGRPC := r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc")
		if isGRPC {
			if strings.EqualFold(forwarded, "edge-grpc.spotify.com") {
				grpcServer.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}

		switch {
		case strings.EqualFold(forwarded, "confidence-resolver-state-cdn.spotifycdn.com"):
			// Route CDN traffic to REST mux (e.g., /state)
			cdn.ServeHTTP(w, r)
			return
		case strings.EqualFold(forwarded, "resolver.confidence.dev"):
			// Route resolver host(s) to gRPC or grpc-gateway
			gw.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	// Apply global HTTP middleware (bandwidth, latency, logging) to all traffic
	var handler http.Handler = base
	if cfg.BandwidthKbps > 0 {
		handler = withHTTPBandwidthLimit(handler, 1024*cfg.BandwidthKbps)
	}
	if cfg.LatencyMs > 0 {
		handler = withHTTPLatency(handler, time.Duration(cfg.LatencyMs)*time.Millisecond)
	}
	if cfg.RequestLogging {
		handler = withHTTPLoggingSkipGRPC(handler)
	}

	httpAddr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("HTTP+h2c (REST+gRPC) listening on %s", httpAddr)
	srv := &http.Server{Addr: httpAddr, Handler: h2c.NewHandler(handler, &http2.Server{})}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("http serve error: %v", err)
	}

}

// withHTTPLoggingSkipGRPC logs only non-gRPC HTTP requests.
func withHTTPLoggingSkipGRPC(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor == 2 && strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			// Bypass HTTP logging for gRPC; gRPC interceptor will log
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Printf("http %s %s status=%d size=%d dur=%s", r.Method, r.URL.RequestURI(), rec.status, rec.size, time.Since(start))
	})
}

// withHTTPLatency sleeps for the provided duration before serving the request.
func withHTTPLatency(next http.Handler, d time.Duration) http.Handler {
	if d <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(d)
		next.ServeHTTP(w, r)
	})
}

// withHTTPBandwidthLimit wraps the ResponseWriter so writes are throttled to approximately bps bytes/sec.
func withHTTPBandwidthLimit(next http.Handler, bps int) http.Handler {
	if bps <= 0 {
		return next
	}
	byteDuration := time.Second / time.Duration(bps)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Throttle request body reads (applies to REST and gRPC over h2c)
		if r.Body != nil {
			r.Body = &throttledReadCloser{
				rc:           r.Body,
				byteDuration: byteDuration,
			}
		}
		tw := &bandwidthWriter{
			ResponseWriter: w,
			byteDuration:   byteDuration,
		}
		next.ServeHTTP(tw, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

// bandwidthWriter throttles writes to an approximate bytes-per-second budget.
type bandwidthWriter struct {
	http.ResponseWriter
	byteDuration time.Duration
}

func (bw *bandwidthWriter) Write(b []byte) (int, error) {
	n, err := bw.ResponseWriter.Write(b)
	if n > 0 && bw.byteDuration > 0 {
		time.Sleep(time.Duration(n) * bw.byteDuration)
	}
	return n, err
}

// Support http.Flusher when present on the underlying writer.
func (bw *bandwidthWriter) Flush() {
	if f, ok := bw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// throttledReadCloser limits Read throughput by sleeping between chunks.
type throttledReadCloser struct {
	rc           io.ReadCloser
	byteDuration time.Duration
}

func (t *throttledReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 && t.byteDuration > 0 {
		time.Sleep(time.Duration(n) * t.byteDuration)
	}
	return n, err
}

func (t *throttledReadCloser) Close() error { return t.rc.Close() }

// gRPC server interceptors for rudimentary request logging.
func unaryLoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	st, _ := status.FromError(err)
	log.Printf("grpc unary %s code=%s dur=%s", info.FullMethod, st.Code(), time.Since(start))
	return resp, err
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v != "" {
		switch v {
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return def
}

func readStateFromUrl(path string) []byte {
	// Blocking HTTP GET read of the provided URL path.
	resp, err := http.Get("https://confidence-resolver-state-cdn.spotifycdn.com/" + path)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("unexpected status code: %d", resp.StatusCode))
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return b
}

func readStateFromDisk(path string, accountId string) []byte {
	// Blocking read from local filesystem.
	b, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	msg := &pb.ClientResolverState{
		State:   b,
		Account: accountId,
	}
	out, err := proto.Marshal(msg)
	if err != nil {
		panic(err)
	}
	return out
}