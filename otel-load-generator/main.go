package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type Config struct {
	CollectorEndpoint string
	LogsPerSecond     int
	DurationSeconds   int
	BatchSize         int
	ExportTimeoutSec  int
	EnableTraces      bool
	Workers           int

	// After rolling independent probabilities, cap the total number of log
	// attribute keys to a value drawn from N(mean, stddev), clamped to [1, fired].
	// 0 means no cap (keep everything that fired).
	AttrKeysMean   float64
	AttrKeysStdDev float64
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloatOrDefault(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func loadConfig() Config {
	return Config{
		CollectorEndpoint: envOrDefault("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		LogsPerSecond:     envIntOrDefault("LOGS_PER_SECOND", 5000),
		DurationSeconds:   envIntOrDefault("DURATION_SECONDS", 60),
		BatchSize:         envIntOrDefault("BATCH_SIZE", 512),
		ExportTimeoutSec:  envIntOrDefault("EXPORT_TIMEOUT_SEC", 30),
		EnableTraces:      envOrDefault("ENABLE_TRACES", "false") == "true",
		Workers:           envIntOrDefault("WORKERS", 8),
		AttrKeysMean:      envFloatOrDefault("ATTR_KEYS_MEAN", 0),
		AttrKeysStdDev:    envFloatOrDefault("ATTR_KEYS_STDDEV", 0),
	}
}

// sampleCap returns the target attribute count. If mean <= 0, returns fired (no cap).
// Otherwise draws from N(mean, stddev) clamped to [1, fired].
func sampleCap(mean, stddev float64, fired int) int {
	if mean <= 0 || fired == 0 {
		return fired
	}
	if stddev <= 0 {
		stddev = 1
	}
	n := mean + stddev*rand.NormFloat64()
	n = math.Round(n)
	if n < 1 {
		n = 1
	}
	if n > float64(fired) {
		n = float64(fired)
	}
	return int(n)
}

// capAttrs randomly removes attributes from the slice until len <= target.
// It does an in-place Fisher-Yates partial shuffle and truncates.
func capAttrs(attrs []log.KeyValue, target int) []log.KeyValue {
	if target >= len(attrs) {
		return attrs
	}
	// Keep `target` random elements
	for i := 0; i < target; i++ {
		j := i + rand.Intn(len(attrs)-i)
		attrs[i], attrs[j] = attrs[j], attrs[i]
	}
	return attrs[:target]
}

// Scope definitions
type scopeDef struct {
	Name    string
	Version string
}

var scopes = []scopeDef{
	{"io.opentelemetry.http", "1.9.0"},
	{"io.opentelemetry.grpc", "1.9.0"},
	{"io.opentelemetry.database", "1.8.0"},
	{"io.opentelemetry.messaging", "1.7.0"},
	{"com.example.app.auth", "2.1.0"},
	{"com.example.app.business", "3.0.1"},
	{"com.example.app.cache", "1.4.2"},
	{"com.example.app.ml", "0.9.5"},
	{"com.example.app.lifecycle", "1.0.0"},
	{"com.example.app.middleware", "2.3.0"},
}

type serviceLogger struct {
	logger  log.Logger
	service ServiceDefinition
}

func buildResource(svc ServiceDefinition) *resource.Resource {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(svc.Name),
		semconv.ServiceNamespace(svc.Namespace),
		semconv.ServiceVersion(svc.Version),
		semconv.ServiceInstanceID(svc.InstanceID),
		semconv.HostName(svc.PodName),
		semconv.HostType(svc.HostType),
		semconv.ContainerID(svc.ContainerID),
		attribute.String("k8s.pod.name", svc.PodName),
		attribute.String("k8s.node.name", svc.NodeName),
		attribute.String("k8s.cluster.name", svc.Cluster),
		attribute.String("deployment.environment", svc.DeploymentEnv),
		attribute.String("team.name", svc.Team),
		attribute.String("service.tier", svc.Tier),
		attribute.String("service.language", svc.Language),
	}
	for k, v := range svc.ExtraAttrs {
		attrs = append(attrs, attribute.String(k, v))
	}
	r, _ := resource.New(context.Background(), resource.WithAttributes(attrs...))
	return r
}

func main() {
	cfg := loadConfig()

	fmt.Println("=== OTel Load Generator (Go) ===")
	fmt.Printf("  Collector:          %s\n", cfg.CollectorEndpoint)
	fmt.Printf("  Target logs/sec:    %d\n", cfg.LogsPerSecond)
	fmt.Printf("  Duration:           %ds\n", cfg.DurationSeconds)
	fmt.Printf("  Batch size:         %d\n", cfg.BatchSize)
	fmt.Printf("  Workers:            %d\n", cfg.Workers)
	fmt.Printf("  Traces enabled:     %v\n", cfg.EnableTraces)
	if cfg.AttrKeysMean > 0 {
		fmt.Printf("  Attr keys cap mean: %.1f\n", cfg.AttrKeysMean)
		fmt.Printf("  Attr keys cap std:  %.1f\n", cfg.AttrKeysStdDev)
	} else {
		fmt.Printf("  Attr keys cap:      none (all fired attrs kept)\n")
	}

	// Count unique attr keys across all templates + global
	uniqueKeys := map[string]bool{}
	for _, t := range logTemplates {
		for _, a := range t.Attrs {
			kv := a.Gen()
			uniqueKeys[kv.Key] = true
		}
	}
	for _, a := range globalAttrs {
		kv := a.Gen()
		uniqueKeys[kv.Key] = true
	}
	uniqueResKeys := map[string]bool{}
	for _, pa := range resPool {
		uniqueResKeys[pa.Key] = true
	}
	fmt.Printf("  Unique log attr keys:  %d (across all templates + global pool)\n", len(uniqueKeys))
	fmt.Printf("  Unique res attr keys:  %d (probability-sampled per service)\n", len(uniqueResKeys)+14) // +14 base
	fmt.Println("================================")
	fmt.Println()

	services := GenerateServiceDefinitions()
	uniqueNames := map[string]bool{}
	for _, s := range services {
		uniqueNames[s.Name] = true
	}
	fmt.Printf("Generated %d service instances across %d unique services\n\n", len(services), len(uniqueNames))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, shutting down...")
		cancel()
	}()

	// Use passthrough resolver so Go's standard net.Resolver handles DNS
	// (respects Docker's /etc/resolv.conf). gRPC's built-in "dns" resolver
	// does not reliably resolve Docker Compose service names.
	target := cfg.CollectorEndpoint
	if !strings.Contains(target, "://") {
		target = "passthrough:///" + target
	}

	fmt.Printf("Waiting for collector at %s...\n", cfg.CollectorEndpoint)
	var (
		conn *grpc.ClientConn
		err  error
	)
	for attempt := 1; attempt <= 30; attempt++ {
		conn, err = grpc.NewClient(target,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create gRPC connection: %v\n", err)
			os.Exit(1)
		}
		// Verify the connection is usable by creating a test exporter
		testCtx, testCancel := context.WithTimeout(ctx, 2*time.Second)
		testExp, testErr := otlploggrpc.New(testCtx, otlploggrpc.WithGRPCConn(conn))
		if testErr == nil {
			_ = testExp.Shutdown(testCtx)
			testCancel()
			fmt.Printf("Collector reachable after %d attempt(s)\n", attempt)
			break
		}
		testCancel()
		if attempt == 30 {
			fmt.Fprintf(os.Stderr, "Could not reach collector after 30 attempts: %v\n", testErr)
			os.Exit(1)
		}
		fmt.Printf("  attempt %d: %v, retrying in 2s...\n", attempt, testErr)
		conn.Close()
		time.Sleep(2 * time.Second)
	}
	defer conn.Close()

	// Create one exporter + batch processor per worker, then round-robin
	// services across them. This avoids 97 independent batch processors
	// all flushing over the same gRPC connection.
	processors := make([]*sdklog.BatchProcessor, cfg.Workers)
	for i := 0; i < cfg.Workers; i++ {
		exp, err := otlploggrpc.New(ctx,
			otlploggrpc.WithGRPCConn(conn),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to create exporter: %v\n", err)
			os.Exit(1)
		}
		processors[i] = sdklog.NewBatchProcessor(exp,
			sdklog.WithMaxQueueSize(8192),
			sdklog.WithExportMaxBatchSize(cfg.BatchSize),
			sdklog.WithExportInterval(500*time.Millisecond),
			sdklog.WithExportTimeout(time.Duration(cfg.ExportTimeoutSec)*time.Second),
		)
	}

	var allLoggers []serviceLogger
	var providers []*sdklog.LoggerProvider

	for i, svc := range services {
		res := buildResource(svc)

		provider := sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(processors[i%cfg.Workers]),
		)
		providers = append(providers, provider)

		scope := scopes[rand.Intn(len(scopes))]
		logger := provider.Logger(scope.Name, log.WithInstrumentationVersion(scope.Version))
		allLoggers = append(allLoggers, serviceLogger{logger: logger, service: svc})
	}

	var totalEmitted atomic.Int64
	startTime := time.Now()

	logsPerBatch := cfg.LogsPerSecond / 10
	if logsPerBatch < 1 {
		logsPerBatch = 1
	}
	logsPerWorkerPerBatch := logsPerBatch / cfg.Workers
	if logsPerWorkerPerBatch < 1 {
		logsPerWorkerPerBatch = 1
	}

	fmt.Printf("Emitting ~%d logs every 100ms across %d workers\n", logsPerBatch, cfg.Workers)
	fmt.Println("Starting load generation...\n")

	var wg sync.WaitGroup
	endTime := startTime.Add(time.Duration(cfg.DurationSeconds) * time.Second)

	// Stats printer
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		lastCount := int64(0)
		lastTime := startTime
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if now.After(endTime) {
					return
				}
				count := totalEmitted.Load()
				elapsed := now.Sub(startTime).Seconds()
				recent := float64(count-lastCount) / now.Sub(lastTime).Seconds()
				overall := float64(count) / elapsed
				fmt.Printf("[%3.0fs] Total: %s logs | Recent: %.0f logs/s | Overall: %.0f logs/s\n",
					elapsed, formatNumber(count), recent, overall)
				lastCount = count
				lastTime = now
			}
		}
	}()

	// Worker goroutines
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					if now.After(endTime) {
						return
					}
						batchTime := now
					millis := batchTime.UnixMilli()
					for i := 0; i < logsPerWorkerPerBatch; i++ {
						sl := allLoggers[rand.Intn(len(allLoggers))]
						tmpl := PickWeightedTemplate(sl.service.Name)

						// Step 1: Roll independent probabilities for template attrs
						attrs := tmpl.GenerateAttrs()

						// Step 2: Roll independent probabilities for global attrs
						for j := range globalAttrs {
							if rand.Float64() < globalAttrs[j].Prob {
								attrs = append(attrs, globalAttrs[j].Gen())
							}
						}

						// Step 3: Always add common attrs
						attrs = append(attrs,
							log.String("log.source", sl.service.Name),
							log.String("environment", "production"),
							log.String("datacenter", sl.service.Region),
							log.String("availability_zone", sl.service.AZ),
							log.String("cluster.name", sl.service.Cluster),
							log.String("host.ip", fmt.Sprintf("10.%d.%d.%d", rand.Intn(255), rand.Intn(255), rand.Intn(255))),
							log.String("correlation.id", fmt.Sprintf("%d-%s", millis, randomHex(8))),
						)

						// Step 4: If mean/stddev set, cap total attrs
						if cfg.AttrKeysMean > 0 {
							target := sampleCap(cfg.AttrKeysMean, cfg.AttrKeysStdDev, len(attrs))
							attrs = capAttrs(attrs, target)
						}

						var rec log.Record
						rec.SetTimestamp(batchTime)
						rec.SetSeverity(tmpl.SeverityNumber)
						rec.SetSeverityText(tmpl.SeverityText)
						rec.SetBody(log.StringValue(tmpl.Body))
						rec.AddAttributes(attrs...)

						sl.logger.Emit(ctx, rec)
						totalEmitted.Add(1)
					}
				}
			}
		}()
	}

	select {
	case <-ctx.Done():
	case <-time.After(time.Until(endTime) + 200*time.Millisecond):
	}
	cancel()
	wg.Wait()

	totalElapsed := time.Since(startTime).Seconds()
	total := totalEmitted.Load()
	fmt.Printf("\n=== Generation Complete ===\n")
	fmt.Printf("Total logs emitted: %s\n", formatNumber(total))
	fmt.Printf("Total time: %.1fs\n", totalElapsed)
	fmt.Printf("Average rate: %.0f logs/s\n", float64(total)/totalElapsed)
	fmt.Println("\nFlushing remaining logs to collector...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	for _, p := range providers {
		if err := p.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: provider shutdown error: %v\n", err)
		}
	}

	fmt.Println("All logs flushed. Done.")
}

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
