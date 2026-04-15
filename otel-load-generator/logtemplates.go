package main

import (
	"fmt"
	"math/rand"
	"strings"

	"go.opentelemetry.io/otel/log"
)

type AttrGenerator func() log.KeyValue

// ProbAttr is an attribute generator with an independent probability of appearing.
type ProbAttr struct {
	Prob float64 // 0.0-1.0 chance of appearing on any given log record
	Gen  AttrGenerator
}

type LogTemplate struct {
	SeverityNumber  log.Severity
	SeverityText    string
	Body            string
	Attrs           []ProbAttr
	Weight          int
	ServicePatterns []string // "*" means all
}

// GenerateAttrs rolls the dice for each ProbAttr and returns the ones that fire.
func (t *LogTemplate) GenerateAttrs() []log.KeyValue {
	out := make([]log.KeyValue, 0, 64)
	for i := range t.Attrs {
		if rand.Float64() < t.Attrs[i].Prob {
			out = append(out, t.Attrs[i].Gen())
		}
	}
	return out
}

func randInt(min, max int) int {
	return min + rand.Intn(max-min+1)
}

func randomIP() string {
	return fmt.Sprintf("%d.%d.%d.%d", randInt(10, 192), randInt(0, 255), randInt(0, 255), randInt(1, 254))
}

func randomUUID() string {
	return fmt.Sprintf("%s-%s-4%s-%s%s-%s",
		randomHex(8), randomHex(4), randomHex(3),
		string("89ab"[rand.Intn(4)]), randomHex(3), randomHex(12))
}

func pickS(arr []string) string {
	return arr[rand.Intn(len(arr))]
}

// Helper constructors for ProbAttr
func ps(prob float64, key string, choices []string) ProbAttr {
	return ProbAttr{prob, func() log.KeyValue { return log.String(key, pickS(choices)) }}
}
func pi(prob float64, key string, min, max int) ProbAttr {
	return ProbAttr{prob, func() log.KeyValue { return log.Int(key, randInt(min, max)) }}
}
func pf(prob float64, key string, maxVal float64) ProbAttr {
	return ProbAttr{prob, func() log.KeyValue {
		return log.Float64(key, float64(int(rand.Float64()*maxVal*100))/100)
	}}
}
func pb(prob float64, key string, trueProb float64) ProbAttr {
	return ProbAttr{prob, func() log.KeyValue { return log.Bool(key, rand.Float64() < trueProb) }}
}
func pfn(prob float64, key string, fn func() string) ProbAttr {
	return ProbAttr{prob, func() log.KeyValue { return log.String(key, fn()) }}
}
func pfnKV(prob float64, fn AttrGenerator) ProbAttr {
	return ProbAttr{prob, fn}
}

// ---------- value pools ----------

var (
	httpMethods     = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	httpPaths       = []string{"/api/v1/users", "/api/v1/products", "/api/v1/orders", "/api/v1/payments", "/api/v1/search", "/api/v1/cart", "/api/v1/checkout", "/api/v1/inventory", "/api/v2/users/{id}", "/api/v2/products/{id}", "/api/v2/orders/{id}", "/api/v1/auth/login", "/api/v1/auth/refresh", "/api/v1/auth/logout", "/api/v1/notifications/send", "/api/v1/recommendations", "/internal/health", "/internal/ready", "/internal/metrics", "/api/v1/reviews", "/api/v1/shipping/rates", "/api/v1/billing/invoices", "/api/v3/graphql", "/api/v1/webhooks", "/api/v1/uploads", "/api/v1/exports"}
	httpStatusCodes = []string{"200", "200", "200", "200", "200", "201", "204", "301", "304", "400", "401", "403", "404", "409", "422", "429", "500", "502", "503", "504"}
	userAgents      = []string{"Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7)", "okhttp/4.12.0", "python-requests/2.31.0", "Go-http-client/2.0", "grpc-java/1.60.0", "axios/1.6.2", "curl/8.4.0", "Apache-HttpClient/4.5.14", "Dart/3.2 (dart:io)", "ReactNative/0.73"}
	dbOps           = []string{"SELECT", "INSERT", "UPDATE", "DELETE", "BEGIN", "COMMIT", "ROLLBACK", "UPSERT"}
	dbTables        = []string{"users", "orders", "products", "sessions", "payments", "inventory", "audit_log", "notifications", "cart_items", "reviews", "subscriptions", "invoices", "shipping_addresses", "wishlists", "coupons"}
	cacheKeys       = []string{"user:profile:", "product:detail:", "session:", "cart:", "inventory:", "search:results:", "config:", "feature:flag:", "rate:limit:", "geo:location:", "price:cache:", "token:blacklist:"}
	queueTopics     = []string{"order.created", "payment.processed", "user.registered", "inventory.updated", "notification.send", "search.index", "analytics.event", "shipping.update", "fraud.check", "email.bounce", "webhook.dispatch", "audit.log"}
	errorTypes      = []string{"ConnectionTimeoutError", "ConnectionRefusedError", "DeadlineExceededError", "ResourceExhaustedError", "PermissionDeniedError", "NotFoundError", "ConflictError", "ValidationError", "InternalError", "UnavailableError", "RateLimitExceededError", "CircuitBreakerOpenError", "RetryableError", "SerializationError", "AuthenticationError", "QuotaExceededError"}
	errorMessages   = []string{"connection timed out after 30s", "resource not found", "permission denied for user", "rate limit exceeded, retry after 60s", "circuit breaker is open", "upstream service unavailable", "invalid request payload", "database connection pool exhausted", "deadlock detected", "max retries exceeded", "TLS handshake failed", "DNS resolution failed", "request body too large", "service mesh sidecar not ready", "certificate expired"}
	rpcServices     = []string{"UserService", "PaymentService", "SearchService", "OrderService", "AuthService", "ProductService", "RecommendationService", "InventoryService", "NotificationService", "ShippingService", "BillingService", "AnalyticsService"}
	rpcMethods      = []string{"Get", "Create", "Update", "Delete", "List", "Search", "Process", "Validate", "BatchGet", "Stream", "Subscribe", "Unsubscribe"}
	authEvents      = []string{"login_success", "login_failure", "token_refresh", "token_validate", "logout", "mfa_challenge", "mfa_verify", "password_reset", "account_locked", "session_expired", "api_key_rotated", "permission_escalation"}
	authMethods     = []string{"password", "oauth2", "saml", "api_key", "mfa_totp", "mfa_sms", "mfa_webauthn", "sso", "certificate"}
	authProviders   = []string{"internal", "google", "github", "okta", "azure_ad", "auth0", "cognito", "keycloak"}
	bizEvents       = []string{"order_placed", "order_completed", "order_cancelled", "payment_captured", "payment_refunded", "payment_failed", "cart_updated", "checkout_started", "checkout_completed", "subscription_created", "subscription_cancelled", "product_viewed", "product_added_to_cart", "coupon_applied", "wishlist_updated", "review_submitted"}
	mlModels        = []string{"fraud-detection-v3", "recommendation-v2", "search-ranking-v4", "pricing-optimizer", "sentiment-analysis", "image-classifier-v2", "churn-predictor", "demand-forecast"}
	mlFrameworks    = []string{"tensorflow", "pytorch", "xgboost", "onnx", "triton", "tensorrt", "scikit-learn"}
	warningTypes    = []string{"slow_query", "high_latency", "connection_pool_low", "memory_pressure", "disk_space_low", "queue_depth_high", "error_rate_elevated", "cpu_throttling", "gc_pressure", "thread_pool_exhaustion", "certificate_expiring", "quota_near_limit", "replication_lag"}
	countryCodes    = []string{"US", "GB", "DE", "FR", "JP", "AU", "CA", "BR", "IN", "KR", "MX", "IT", "ES", "NL", "SE", "SG"}
	cities          = []string{"New York", "London", "Berlin", "Tokyo", "Sydney", "Toronto", "São Paulo", "Seoul", "Amsterdam", "Singapore", "Paris", "Mumbai", "Chicago", "Dallas", "Seattle"}
	currencies      = []string{"USD", "EUR", "GBP", "JPY", "AUD", "CAD", "BRL", "KRW", "INR", "SGD"}
	payMethods      = []string{"credit_card", "debit_card", "paypal", "apple_pay", "google_pay", "bank_transfer", "crypto", "klarna", "afterpay"}
	categories      = []string{"electronics", "clothing", "food", "home", "sports", "books", "toys", "beauty", "automotive", "garden", "health", "office"}
	channels        = []string{"web", "mobile_app", "api", "in_store", "kiosk", "voice_assistant", "chatbot"}
	contentTypes    = []string{"application/json", "application/xml", "application/protobuf", "text/html", "multipart/form-data", "application/octet-stream", "text/plain", "application/grpc"}
	tlsVersions     = []string{"TLSv1.2", "TLSv1.3"}
	tlsCiphers      = []string{"TLS_AES_256_GCM_SHA384", "TLS_CHACHA20_POLY1305_SHA256", "TLS_AES_128_GCM_SHA256", "ECDHE-RSA-AES128-GCM-SHA256"}
	dnsTypes        = []string{"A", "AAAA", "CNAME", "SRV", "TXT"}
	featureFlags    = []string{"new-checkout-flow", "dark-mode", "recommendation-v2", "parallel-search", "grpc-migration", "canary-deploy", "rate-limit-v2", "async-notifications", "ml-ranking", "edge-caching", "ab-test-pricing", "graphql-gateway"}
	abTestGroups    = []string{"control", "variant_a", "variant_b", "variant_c", "holdout"}
	logLevels       = []string{"TRACE", "DEBUG", "INFO", "WARN", "ERROR", "FATAL"}
	networkProtos   = []string{"tcp", "udp", "http", "http2", "grpc", "websocket", "quic"}
	k8sEvents       = []string{"Pulling", "Pulled", "Created", "Started", "Killing", "BackOff", "FailedScheduling", "Unhealthy", "SuccessfulCreate"}
	svcMeshActions  = []string{"allow", "deny", "passthrough", "rate_limited", "circuit_broken", "retried"}
	compressAlgos   = []string{"gzip", "zstd", "lz4", "snappy", "brotli", "none"}
	samplerTypes    = []string{"always_on", "always_off", "trace_id_ratio", "parent_based", "jaeger_remote"}
)

func genStackTrace() string {
	frames := randInt(3, 15)
	funcs := []string{"handle", "process", "execute", "validate", "transform", "fetch", "send", "parse", "dispatch", "route", "serialize", "decode", "authenticate", "authorize"}
	files := []string{"handler", "service", "repository", "client", "middleware", "controller", "gateway", "proxy", "interceptor", "resolver"}
	exts := []string{"go", "java", "py", "ts", "rs", "kt"}
	var sb strings.Builder
	for i := 0; i < frames; i++ {
		fmt.Fprintf(&sb, "  at %s(%s.%s:%d)\n", pickS(funcs), pickS(files), pickS(exts), randInt(10, 500))
	}
	return sb.String()
}

func genDBStatement() string {
	op := pickS(dbOps)
	table := pickS(dbTables)
	switch op {
	case "SELECT":
		return fmt.Sprintf("SELECT * FROM %s WHERE id = $1 LIMIT 100", table)
	case "INSERT":
		return fmt.Sprintf("INSERT INTO %s (col1, col2, col3) VALUES ($1, $2, $3) RETURNING id", table)
	case "UPDATE":
		return fmt.Sprintf("UPDATE %s SET updated_at = NOW(), status = $2 WHERE id = $1", table)
	case "UPSERT":
		return fmt.Sprintf("INSERT INTO %s (id, col1) VALUES ($1, $2) ON CONFLICT (id) DO UPDATE SET col1 = $2", table)
	default:
		return fmt.Sprintf("%s FROM %s WHERE id = $1", op, table)
	}
}

// ---------- template attribute pools ----------

// Each template function returns a big pool of ProbAttr. The probability on each
// attribute controls how often it shows up, giving natural variance per record.

func httpRequestPool() []ProbAttr {
	return []ProbAttr{
		// ---- near-always (0.90-0.99) ----
		ps(0.99, "http.request.method", httpMethods),
		ps(0.98, "http.route", httpPaths),
		ps(0.97, "http.response.status_code", httpStatusCodes),
		pfn(0.95, "request.id", randomUUID),
		pf(0.95, "http.request.duration_ms", 5000),
		ps(0.92, "url.scheme", []string{"http", "https"}),
		ps(0.90, "network.protocol.version", []string{"1.1", "2.0", "3.0"}),
		// ---- common (0.60-0.89) ----
		pi(0.88, "http.response.body.size", 64, 524288),
		pi(0.85, "http.request.body.size", 0, 65536),
		pfn(0.82, "client.address", randomIP),
		pi(0.80, "client.port", 1024, 65535),
		ps(0.78, "server.address", []string{"api.example.com", "svc.example.com", "internal.example.com", "edge.example.com"}),
		pi(0.75, "server.port", 80, 8443),
		ps(0.72, "user_agent.original", userAgents),
		pfn(0.70, "http.request.header.x_request_id", randomUUID),
		pfn(0.68, "session.id", randomUUID),
		pfnKV(0.65, func() log.KeyValue { return log.String("user.id", fmt.Sprintf("usr_%d", randInt(100000, 999999))) }),
		pfn(0.60, "http.request.header.x_forwarded_for", randomIP),
		// ---- moderate (0.30-0.59) ----
		ps(0.55, "http.request.header.content_type", contentTypes),
		ps(0.52, "http.response.header.content_type", contentTypes),
		pfn(0.50, "http.request.header.x_correlation_id", randomUUID),
		ps(0.48, "net.host.name", []string{"gateway-01", "gateway-02", "gateway-03", "gateway-04", "gateway-05"}),
		pi(0.45, "http.request.header.content_length", 0, 1048576),
		ps(0.42, "http.flavor", []string{"1.0", "1.1", "2", "3"}),
		pfn(0.40, "http.request.header.x_trace_id", func() string { return randomHex(32) }),
		pfn(0.38, "http.request.header.x_span_id", func() string { return randomHex(16) }),
		ps(0.36, "http.request.header.accept", []string{"application/json", "*/*", "text/html", "application/xml"}),
		ps(0.35, "http.request.header.accept_encoding", compressAlgos),
		ps(0.33, "http.request.header.accept_language", []string{"en-US", "en-GB", "de-DE", "fr-FR", "ja-JP", "es-ES", "pt-BR"}),
		ps(0.32, "tls.version", tlsVersions),
		ps(0.30, "tls.cipher", tlsCiphers),
		// ---- uncommon (0.10-0.29) ----
		pfn(0.28, "http.request.header.x_amzn_trace_id", func() string { return fmt.Sprintf("Root=1-%s-%s", randomHex(8), randomHex(24)) }),
		pfn(0.25, "http.request.header.authorization_type", func() string { return pickS([]string{"Bearer", "Basic", "ApiKey", "Digest"}) }),
		pi(0.22, "http.retry_count", 0, 5),
		pb(0.20, "http.request.tls_established", 0.95),
		pfn(0.18, "http.request.header.x_real_ip", randomIP),
		ps(0.16, "http.request.header.x_forwarded_proto", []string{"http", "https"}),
		pi(0.15, "http.request.header.x_ratelimit_limit", 100, 10000),
		pi(0.15, "http.request.header.x_ratelimit_remaining", 0, 10000),
		pi(0.14, "http.request.header.x_ratelimit_reset", 1000000000, 2000000000),
		pf(0.13, "http.server.request.processing_ms", 100),
		pf(0.12, "http.server.request.queue_time_ms", 50),
		ps(0.12, "http.connection.state", []string{"new", "reused", "idle", "closing"}),
		pi(0.10, "http.connection.id", 1, 100000),
		// ---- rare (0.01-0.09) ----
		pfn(0.08, "http.request.header.x_debug_id", randomUUID),
		ps(0.07, "http.request.header.x_ab_test", abTestGroups),
		ps(0.06, "http.request.header.x_feature_flag", featureFlags),
		pi(0.05, "http.request.header.x_priority", 1, 10),
		ps(0.05, "http.response.header.x_cache", []string{"HIT", "MISS", "BYPASS", "STALE", "UPDATING", "REVALIDATED"}),
		pf(0.04, "http.response.header.x_response_time", 10000),
		pfn(0.04, "http.response.header.x_served_by", func() string { return fmt.Sprintf("node-%d", randInt(1, 100)) }),
		ps(0.03, "http.response.header.x_frame_options", []string{"DENY", "SAMEORIGIN"}),
		ps(0.03, "http.response.header.x_content_type_options", []string{"nosniff"}),
		pfn(0.02, "http.request.header.x_idempotency_key", randomUUID),
		pfn(0.02, "http.request.header.x_client_version", func() string { return fmt.Sprintf("%d.%d.%d", randInt(1, 5), randInt(0, 20), randInt(0, 100)) }),
		pb(0.01, "http.request.is_synthetic", 1.0),
		ps(0.01, "http.request.header.x_canary", []string{"true", "false"}),
		pfn(0.008, "http.request.header.x_shadow_traffic", func() string { return fmt.Sprintf("shadow-%s", randomHex(8)) }),
		pb(0.005, "http.request.header.x_load_test", 1.0),
	}
}

func dbQueryPool() []ProbAttr {
	return []ProbAttr{
		// ---- near-always ----
		ps(0.99, "db.system", []string{"postgresql", "mysql", "mongodb", "cockroachdb", "vitess"}),
		ps(0.98, "db.operation", dbOps),
		ps(0.97, "db.sql.table", dbTables),
		pf(0.96, "db.query.duration_ms", 2000),
		ps(0.95, "db.name", []string{"users_db", "orders_db", "products_db", "payments_db", "inventory_db", "analytics_db", "sessions_db"}),
		// ---- common ----
		pfn(0.85, "db.statement", genDBStatement),
		pi(0.82, "db.rows_affected", 0, 5000),
		pi(0.78, "db.connection_pool.active", 1, 100),
		pi(0.75, "db.connection_pool.idle", 0, 50),
		pi(0.72, "db.connection_pool.max", 20, 200),
		ps(0.70, "db.host", []string{"db-primary.internal", "db-replica-1.internal", "db-replica-2.internal", "db-replica-3.internal", "db-readonly.internal"}),
		pi(0.68, "db.port", 3306, 27017),
		ps(0.65, "db.user", []string{"app_rw", "app_ro", "migration_user", "replication_user", "analytics_ro"}),
		pi(0.62, "thread.id", 1, 500),
		pfn(0.60, "thread.name", func() string { return fmt.Sprintf("db-pool-%d", randInt(1, 50)) }),
		// ---- moderate ----
		ps(0.55, "db.connection.state", []string{"active", "idle", "waiting", "closing"}),
		pi(0.50, "db.connection_pool.wait_count", 0, 100),
		pf(0.48, "db.connection_pool.wait_time_ms", 5000),
		pi(0.45, "db.rows_returned", 0, 10000),
		pfn(0.42, "db.query.plan_hash", func() string { return randomHex(16) }),
		ps(0.40, "db.transaction.isolation_level", []string{"read_committed", "repeatable_read", "serializable", "read_uncommitted"}),
		pf(0.38, "db.query.planning_time_ms", 50),
		pf(0.35, "db.query.execution_time_ms", 2000),
		pi(0.33, "db.query.shared_blocks_hit", 0, 100000),
		pi(0.30, "db.query.shared_blocks_read", 0, 10000),
		// ---- uncommon ----
		pb(0.25, "db.query.used_index", 0.8),
		ps(0.22, "db.query.index_name", []string{"idx_users_email", "idx_orders_user_id", "idx_products_sku", "idx_payments_order_id", "pk_primary", "idx_created_at", "idx_composite_1"}),
		pi(0.20, "db.query.temp_bytes_written", 0, 10485760),
		pf(0.18, "db.replication.lag_ms", 5000),
		ps(0.16, "db.replication.role", []string{"primary", "replica", "standby"}),
		pi(0.15, "db.connection_pool.checkout_count", 0, 100000),
		pf(0.14, "db.connection_pool.checkout_time_ms", 100),
		pb(0.12, "db.query.is_prepared", 0.7),
		ps(0.10, "db.query.cache_hit", []string{"true", "false"}),
		// ---- rare ----
		pb(0.08, "db.query.seq_scan", 0.3),
		pi(0.06, "db.deadlock_count", 0, 5),
		pfn(0.05, "db.advisory_lock_id", func() string { return fmt.Sprintf("lock_%d", randInt(1, 1000)) }),
		pi(0.04, "db.query.parameters_count", 0, 50),
		pb(0.03, "db.query.is_slow", 1.0),
		pf(0.02, "db.vacuum.last_run_seconds_ago", 86400),
		pi(0.02, "db.table.estimated_rows", 1000, 100000000),
		pf(0.01, "db.table.bloat_ratio", 2),
		pb(0.008, "db.failover_in_progress", 1.0),
		pfn(0.005, "db.migration.version", func() string { return fmt.Sprintf("2024%02d%02d_%03d", randInt(1, 12), randInt(1, 28), randInt(1, 999)) }),
	}
}

func cachePool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "cache.system", []string{"redis", "memcached", "hazelcast", "aerospike"}),
		ps(0.98, "cache.operation", []string{"GET", "SET", "DEL", "MGET", "MSET", "EXPIRE", "INCR", "DECR", "HGET", "HSET", "LPUSH", "RPOP", "SADD", "ZADD", "SCAN"}),
		pb(0.95, "cache.hit", 0.7),
		pf(0.93, "cache.latency_ms", 50),
		ps(0.90, "cache.key_prefix", cacheKeys),
		// ---- common ----
		pi(0.82, "cache.value_size_bytes", 1, 1048576),
		pi(0.78, "cache.ttl_seconds", 1, 604800),
		ps(0.75, "cache.cluster", []string{"cache-main", "cache-session", "cache-hot", "cache-ephemeral", "cache-persistent"}),
		pfn(0.72, "cache.node", func() string { return fmt.Sprintf("redis-%d.cache.internal", randInt(1, 12)) }),
		pi(0.68, "cache.db_index", 0, 15),
		pi(0.65, "cache.connection_pool.size", 5, 200),
		pi(0.62, "cache.connection_pool.active", 1, 200),
		// ---- moderate ----
		ps(0.55, "cache.serialization_format", []string{"json", "msgpack", "protobuf", "gob", "raw"}),
		pi(0.50, "cache.pipeline_depth", 1, 100),
		pf(0.48, "cache.memory_usage_percent", 100),
		pfn(0.45, "cache.key_hash", func() string { return randomHex(16) }),
		pi(0.42, "cache.key_length", 5, 256),
		pb(0.40, "cache.compressed", 0.3),
		ps(0.38, "cache.eviction_policy", []string{"lru", "lfu", "random", "ttl", "volatile-lru", "allkeys-lru"}),
		pi(0.35, "cache.slot", 0, 16383),
		// ---- uncommon ----
		pi(0.25, "cache.network.bytes_sent", 0, 1048576),
		pi(0.22, "cache.network.bytes_received", 0, 1048576),
		pf(0.20, "cache.cpu_time_us", 10000),
		pi(0.18, "cache.keyspace.hits", 0, 1000000),
		pi(0.16, "cache.keyspace.misses", 0, 500000),
		pf(0.14, "cache.fragmentation_ratio", 3),
		pi(0.12, "cache.connected_clients", 1, 500),
		pb(0.10, "cache.is_cluster_mode", 0.5),
		// ---- rare ----
		pi(0.07, "cache.replication.offset", 0, 1000000000),
		pb(0.05, "cache.aof_enabled", 0.6),
		pf(0.04, "cache.rdb_last_save_seconds_ago", 86400),
		pi(0.03, "cache.pubsub.channels", 0, 100),
		pb(0.02, "cache.readonly_replica", 0.3),
		pf(0.01, "cache.slowlog_entry_ms", 100),
		pb(0.008, "cache.maxmemory_reached", 1.0),
		pfn(0.005, "cache.sentinel.master_name", func() string { return fmt.Sprintf("mymaster-%d", randInt(1, 5)) }),
	}
}

func messageQueuePool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "messaging.system", []string{"kafka", "rabbitmq", "sqs", "nats", "pulsar", "redpanda"}),
		ps(0.98, "messaging.operation", []string{"publish", "receive", "process", "ack", "nack", "reject", "dead_letter"}),
		ps(0.97, "messaging.destination.name", queueTopics),
		pfn(0.95, "messaging.message.id", randomUUID),
		pi(0.90, "messaging.message.body.size", 10, 1048576),
		// ---- common ----
		pi(0.82, "messaging.kafka.partition", 0, 31),
		pi(0.80, "messaging.kafka.offset", 0, 100000000),
		ps(0.78, "messaging.kafka.consumer_group", []string{"cg-orders", "cg-payments", "cg-analytics", "cg-search", "cg-notifications", "cg-fraud", "cg-shipping"}),
		pf(0.75, "messaging.processing.duration_ms", 5000),
		pi(0.72, "messaging.batch.size", 1, 500),
		pi(0.68, "messaging.retry_count", 0, 10),
		pfn(0.65, "messaging.correlation_id", randomUUID),
		// ---- moderate ----
		ps(0.55, "messaging.destination.kind", []string{"topic", "queue", "subscription"}),
		ps(0.50, "messaging.message.content_type", contentTypes),
		ps(0.48, "messaging.compression", compressAlgos),
		pi(0.45, "messaging.kafka.consumer_lag", 0, 1000000),
		pfn(0.42, "messaging.message.key", func() string { return fmt.Sprintf("key-%s", randomHex(8)) }),
		pf(0.40, "messaging.message.enqueue_time_ms", 10000),
		ps(0.38, "messaging.kafka.acks", []string{"0", "1", "all"}),
		pi(0.35, "messaging.destination.partition_count", 1, 64),
		// ---- uncommon ----
		pi(0.25, "messaging.kafka.replication_factor", 1, 5),
		pf(0.22, "messaging.delivery_latency_ms", 30000),
		pi(0.20, "messaging.message.header_count", 0, 20),
		ps(0.18, "messaging.protocol.version", []string{"0.10.2", "2.0", "2.8", "3.0", "3.6"}),
		pb(0.15, "messaging.is_poison_pill", 0.01),
		pi(0.12, "messaging.dlq.retry_count", 0, 10),
		ps(0.10, "messaging.consumer.state", []string{"running", "paused", "rebalancing", "stopped"}),
		// ---- rare ----
		pb(0.07, "messaging.transaction.active", 0.1),
		pfn(0.05, "messaging.transaction.id", randomUUID),
		pf(0.04, "messaging.consumer.rebalance_time_ms", 30000),
		pi(0.03, "messaging.schema_registry.schema_id", 1, 10000),
		ps(0.02, "messaging.schema_registry.subject", []string{"order-value", "payment-value", "user-value", "event-value"}),
		pb(0.01, "messaging.exactly_once_enabled", 0.5),
		pf(0.008, "messaging.broker.disk_usage_percent", 100),
		pb(0.005, "messaging.unclean_leader_election", 1.0),
	}
}

func grpcPool() []ProbAttr {
	return []ProbAttr{
		pfnKV(0.99, func() log.KeyValue { return log.String("rpc.system", "grpc") }),
		ps(0.98, "rpc.service", rpcServices),
		ps(0.97, "rpc.method", rpcMethods),
		pi(0.95, "rpc.grpc.status_code", 0, 16),
		pf(0.93, "rpc.duration_ms", 5000),
		pfn(0.90, "rpc.grpc.request.metadata.x_request_id", randomUUID),
		// ---- common ----
		ps(0.82, "rpc.message.type", []string{"SENT", "RECEIVED"}),
		pi(0.80, "rpc.message.compressed_size", 10, 1048576),
		pi(0.78, "rpc.message.uncompressed_size", 100, 2097152),
		ps(0.75, "peer.service", []string{"user-service", "payment-processor", "search-api", "order-service", "auth-service", "inventory-service", "notification-router", "billing-service"}),
		ps(0.72, "net.peer.name", []string{"user-svc.internal", "payment-svc.internal", "search-svc.internal", "order-svc.internal", "auth-svc.internal", "inventory-svc.internal"}),
		pi(0.70, "net.peer.port", 8081, 50051),
		ps(0.68, "rpc.grpc.request.metadata.content_type", []string{"application/grpc", "application/grpc+proto"}),
		// ---- moderate ----
		pfn(0.55, "rpc.grpc.request.metadata.x_b3_traceid", func() string { return randomHex(32) }),
		pfn(0.52, "rpc.grpc.request.metadata.x_b3_spanid", func() string { return randomHex(16) }),
		ps(0.50, "rpc.grpc.request.metadata.x_b3_sampled", []string{"0", "1"}),
		ps(0.48, "rpc.grpc.compression", compressAlgos),
		pf(0.45, "rpc.grpc.request.metadata.deadline_ms", 30000),
		pi(0.42, "rpc.message.id", 1, 10000),
		pfn(0.40, "rpc.grpc.request.metadata.authority", func() string { return fmt.Sprintf("%s.internal:443", pickS([]string{"user", "payment", "search", "order", "auth"})) }),
		pi(0.38, "rpc.message.sequence_no", 1, 1000),
		// ---- uncommon ----
		pb(0.25, "rpc.grpc.is_streaming", 0.2),
		pi(0.22, "rpc.grpc.request.metadata.max_recv_message_size", 1048576, 67108864),
		pi(0.20, "rpc.grpc.request.metadata.max_send_message_size", 1048576, 67108864),
		pf(0.18, "rpc.grpc.keepalive_time_ms", 60000),
		pi(0.15, "rpc.grpc.concurrent_streams", 1, 250),
		ps(0.12, "rpc.grpc.load_balancing_policy", []string{"round_robin", "pick_first", "grpclb", "xds"}),
		pb(0.10, "rpc.grpc.retry_enabled", 0.8),
		// ---- rare ----
		pi(0.07, "rpc.grpc.retry_count", 0, 5),
		pfn(0.05, "rpc.grpc.request.metadata.x_envoy_attempt_count", func() string { return fmt.Sprintf("%d", randInt(1, 5)) }),
		pf(0.04, "rpc.grpc.backoff_ms", 5000),
		ps(0.03, "rpc.grpc.service_config_source", []string{"dns", "xds", "static", "file"}),
		pb(0.02, "rpc.grpc.is_hedged", 0.1),
		pfn(0.01, "rpc.grpc.xds.cluster", func() string { return fmt.Sprintf("xds_cluster_%s", randomHex(4)) }),
		pb(0.005, "rpc.grpc.reflection_used", 1.0),
	}
}

func lifecyclePool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "lifecycle.event", []string{"startup", "ready", "health_check", "config_reload", "graceful_shutdown", "liveness_probe", "readiness_probe", "startup_probe", "pre_stop_hook", "post_start_hook"}),
		pi(0.95, "lifecycle.uptime_seconds", 0, 2592000),
		pfn(0.90, "lifecycle.version", func() string { return fmt.Sprintf("1.%d.%d", randInt(0, 30), randInt(0, 100)) }),
		// ---- common ----
		pi(0.82, "process.memory.rss_bytes", 50000000, 8000000000),
		pf(0.80, "process.cpu.utilization", 1),
		pi(0.78, "gc.heap_size_bytes", 100000000, 4000000000),
		pf(0.75, "gc.pause_ms", 500),
		pf(0.72, "system.load.1m", 16),
		pf(0.70, "system.load.5m", 16),
		pf(0.68, "system.load.15m", 16),
		pfn(0.65, "lifecycle.config_hash", func() string { return fmt.Sprintf("sha256:%s", randomHex(32)) }),
		// ---- moderate ----
		pi(0.55, "process.memory.heap_alloc_bytes", 50000000, 4000000000),
		pi(0.52, "process.memory.heap_sys_bytes", 100000000, 8000000000),
		pi(0.50, "process.memory.stack_bytes", 1000000, 100000000),
		pi(0.48, "gc.count", 0, 100000),
		pf(0.45, "gc.total_pause_ms", 100000),
		pi(0.42, "process.goroutine_count", 1, 10000),
		pi(0.40, "process.thread_count", 1, 500),
		pi(0.38, "process.fd_count", 10, 65535),
		pi(0.35, "process.fd_limit", 1024, 1048576),
		pf(0.33, "system.memory.utilization", 1),
		pi(0.30, "system.memory.total_bytes", 1000000000, 64000000000),
		// ---- uncommon ----
		pi(0.25, "system.disk.total_bytes", 10000000000, 1000000000000),
		pf(0.22, "system.disk.utilization", 1),
		pf(0.20, "system.network.bytes_sent_rate", 1000000000),
		pf(0.18, "system.network.bytes_recv_rate", 1000000000),
		pi(0.16, "system.cpu.count", 1, 64),
		pi(0.14, "k8s.container.restart_count", 0, 10),
		ps(0.12, "k8s.container.status", []string{"running", "waiting", "terminated"}),
		ps(0.10, "k8s.pod.phase", []string{"Pending", "Running", "Succeeded", "Failed"}),
		// ---- rare ----
		pi(0.07, "process.memory.gc_goal_bytes", 100000000, 8000000000),
		pf(0.05, "gc.cpu_fraction", 0.1),
		pb(0.04, "process.cgroup.memory_limit_hit", 0.1),
		pi(0.03, "process.cgroup.cpu_throttled_periods", 0, 10000),
		pf(0.02, "process.cgroup.cpu_throttled_time_ms", 60000),
		pb(0.01, "process.oom_kill_detected", 0.05),
		pfn(0.008, "k8s.event", func() string { return pickS(k8sEvents) }),
		pfn(0.005, "lifecycle.signal_received", func() string { return pickS([]string{"SIGTERM", "SIGINT", "SIGHUP", "SIGUSR1"}) }),
	}
}

func authPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "auth.event", authEvents),
		ps(0.97, "auth.method", authMethods),
		ps(0.95, "auth.provider", authProviders),
		pfnKV(0.93, func() log.KeyValue { return log.String("user.id", fmt.Sprintf("usr_%d", randInt(100000, 999999))) }),
		// ---- common ----
		ps(0.82, "user.roles", []string{"admin", "user", "editor", "viewer", "service_account", "super_admin", "billing_admin", "support"}),
		ps(0.80, "auth.token.type", []string{"access_token", "refresh_token", "id_token", "api_key", "service_token"}),
		pi(0.78, "auth.token.expires_in", 60, 604800),
		ps(0.75, "auth.scope", []string{"read", "write", "admin", "read write", "openid profile email", "api.full", "api.read"}),
		pfnKV(0.72, func() log.KeyValue { return log.String("auth.client_id", fmt.Sprintf("client_%d", randInt(1000, 9999))) }),
		ps(0.70, "user.email_domain", []string{"gmail.com", "company.com", "outlook.com", "yahoo.com", "hotmail.com", "protonmail.com", "corporate.internal"}),
		ps(0.68, "geo.country_code", countryCodes),
		ps(0.65, "geo.city", cities),
		pfn(0.62, "client.address", randomIP),
		pf(0.60, "auth.ip_reputation_score", 1),
		// ---- moderate ----
		pfn(0.55, "auth.session.id", randomUUID),
		pi(0.52, "auth.session.age_seconds", 0, 86400),
		ps(0.50, "auth.mfa.type", []string{"totp", "sms", "webauthn", "push", "email", "recovery_code"}),
		pb(0.48, "auth.mfa.verified", 0.95),
		ps(0.45, "user.account_type", []string{"personal", "business", "enterprise", "trial", "free"}),
		pfn(0.42, "auth.device.fingerprint", func() string { return randomHex(32) }),
		ps(0.40, "auth.device.type", []string{"desktop", "mobile", "tablet", "api_client", "cli", "sdk"}),
		ps(0.38, "auth.device.os", []string{"iOS", "Android", "Windows", "macOS", "Linux", "ChromeOS"}),
		ps(0.35, "auth.device.browser", []string{"Chrome", "Firefox", "Safari", "Edge", "Opera", "curl", "Postman"}),
		pi(0.33, "auth.failed_attempts", 0, 10),
		pb(0.30, "auth.password_expired", 0.05),
		// ---- uncommon ----
		pfn(0.25, "auth.token.jti", randomUUID),
		pfn(0.22, "auth.token.fingerprint", func() string { return randomHex(16) }),
		pb(0.20, "auth.is_impersonation", 0.02),
		pfn(0.18, "auth.impersonator.id", func() string { return fmt.Sprintf("usr_%d", randInt(100000, 999999)) }),
		ps(0.16, "auth.risk_level", []string{"low", "medium", "high", "critical"}),
		ps(0.14, "auth.policy.name", []string{"default", "strict", "mfa-required", "geo-restricted", "ip-whitelist"}),
		pb(0.12, "auth.consent_required", 0.3),
		pb(0.10, "auth.session.is_remembered", 0.6),
		// ---- rare ----
		pb(0.07, "auth.account_locked", 0.02),
		pfn(0.05, "auth.lockout_until", func() string { return fmt.Sprintf("2026-04-%02dT%02d:%02d:00Z", randInt(1, 28), randInt(0, 23), randInt(0, 59)) }),
		pb(0.04, "auth.brute_force_detected", 0.01),
		ps(0.03, "auth.compliance.regulation", []string{"GDPR", "CCPA", "SOC2", "HIPAA", "PCI-DSS"}),
		pb(0.02, "auth.suspicious_location", 0.05),
		pfn(0.01, "auth.previous_country_code", func() string { return pickS(countryCodes) }),
		pb(0.008, "auth.impossible_travel_detected", 0.01),
		pb(0.005, "auth.credential_stuffing_detected", 0.005),
	}
}

func errorPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "error.type", errorTypes),
		ps(0.98, "error.message", errorMessages),
		pfn(0.95, "request.id", randomUUID),
		// ---- common ----
		pfn(0.82, "error.stack_trace", genStackTrace),
		pi(0.80, "error.retry_count", 0, 10),
		pb(0.78, "error.is_retryable", 0.6),
		ps(0.75, "span.kind", []string{"server", "client", "internal", "producer", "consumer"}),
		pb(0.72, "exception.escaped", 0.3),
		ps(0.70, "error.severity", []string{"low", "medium", "high", "critical"}),
		pfn(0.68, "error.correlation_id", randomUUID),
		// ---- moderate ----
		ps(0.55, "error.component", []string{"http_handler", "grpc_handler", "db_client", "cache_client", "queue_consumer", "external_api", "middleware", "serializer", "auth_provider"}),
		ps(0.52, "error.category", []string{"network", "database", "authentication", "validation", "timeout", "resource_exhaustion", "configuration", "dependency"}),
		pf(0.50, "error.duration_ms", 30000),
		ps(0.48, "error.endpoint", httpPaths),
		pfnKV(0.45, func() log.KeyValue { return log.String("error.user_id", fmt.Sprintf("usr_%d", randInt(100000, 999999))) }),
		ps(0.42, "error.http_status", []string{"400", "401", "403", "404", "409", "422", "429", "500", "502", "503", "504"}),
		ps(0.40, "error.grpc_code", []string{"CANCELLED", "UNKNOWN", "INVALID_ARGUMENT", "DEADLINE_EXCEEDED", "NOT_FOUND", "ALREADY_EXISTS", "PERMISSION_DENIED", "RESOURCE_EXHAUSTED", "INTERNAL", "UNAVAILABLE"}),
		pb(0.38, "error.is_expected", 0.3),
		ps(0.35, "error.handling", []string{"retry", "fallback", "circuit_break", "dead_letter", "alert", "ignore", "escalate"}),
		pfn(0.33, "error.fingerprint", func() string { return randomHex(16) }),
		// ---- uncommon ----
		pi(0.25, "error.occurrence_count", 1, 10000),
		pfn(0.22, "error.first_seen", func() string { return fmt.Sprintf("2026-03-%02dT%02d:%02d:00Z", randInt(1, 31), randInt(0, 23), randInt(0, 59)) }),
		pb(0.20, "error.is_new", 0.1),
		ps(0.18, "error.alert.channel", []string{"pagerduty", "slack", "opsgenie", "email", "teams"}),
		ps(0.16, "error.alert.priority", []string{"P1", "P2", "P3", "P4", "P5"}),
		pfn(0.14, "error.alert.runbook_url", func() string { return fmt.Sprintf("https://runbooks.internal/errors/%s", randomHex(8)) }),
		ps(0.12, "error.downstream_service", []string{"user-service", "payment-processor", "search-api", "order-service", "auth-service", "external-partner-api"}),
		pf(0.10, "error.downstream_latency_ms", 30000),
		// ---- rare ----
		pb(0.07, "error.circuit_breaker.open", 0.3),
		pi(0.05, "error.circuit_breaker.failure_count", 0, 100),
		pf(0.04, "error.circuit_breaker.reset_timeout_ms", 60000),
		pb(0.03, "error.is_incident", 0.05),
		pfn(0.02, "error.incident_id", func() string { return fmt.Sprintf("INC-%d", randInt(10000, 99999)) }),
		pb(0.01, "error.customer_facing", 0.4),
		pb(0.008, "error.data_loss_risk", 0.01),
		pb(0.005, "error.requires_manual_intervention", 0.02),
	}
}

func warningPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "warning.type", warningTypes),
		pi(0.97, "warning.threshold", 10, 100000),
		pi(0.95, "warning.current_value", 10, 200000),
		ps(0.93, "warning.unit", []string{"ms", "percent", "bytes", "count", "per_second", "connections", "megabytes"}),
		// ---- common ----
		pi(0.82, "warning.duration_seconds", 1, 7200),
		ps(0.80, "warning.affected_endpoint", httpPaths),
		pfn(0.78, "request.id", randomUUID),
		ps(0.75, "alert.severity", []string{"p1", "p2", "p3", "p4", "p5"}),
		pfn(0.72, "alert.runbook_url", func() string { return fmt.Sprintf("https://runbooks.internal/alerts/%s", pickS([]string{"latency", "errors", "resources", "capacity", "security", "replication", "memory", "disk"})) }),
		ps(0.70, "warning.source", []string{"self_monitor", "health_check", "metric_alert", "log_pattern", "anomaly_detector"}),
		// ---- moderate ----
		ps(0.55, "warning.component", []string{"http_server", "grpc_server", "database", "cache", "queue", "disk", "memory", "cpu", "network", "dns", "tls"}),
		pf(0.52, "warning.trend_direction", 1),
		pi(0.50, "warning.percentile", 50, 99),
		pf(0.48, "warning.baseline_value", 100000),
		pf(0.45, "warning.deviation_factor", 10),
		pb(0.42, "warning.auto_remediated", 0.3),
		ps(0.40, "warning.remediation_action", []string{"scale_up", "restart", "failover", "rate_limit", "circuit_break", "none"}),
		pfn(0.38, "warning.affected_service", func() string { return pickS([]string{"user-service", "payment-processor", "search-api", "order-service", "auth-service", "cache-proxy"}) }),
		pi(0.35, "warning.affected_instances", 1, 50),
		pf(0.33, "warning.impact_score", 10),
		// ---- uncommon ----
		ps(0.25, "warning.alert_group", []string{"infrastructure", "application", "business", "security", "compliance"}),
		pi(0.22, "warning.consecutive_violations", 1, 100),
		pf(0.20, "warning.time_to_breach_seconds", 3600),
		pb(0.18, "warning.silenced", 0.15),
		pfn(0.16, "warning.silence_reason", func() string { return pickS([]string{"maintenance_window", "known_issue", "deploy_in_progress", "false_positive"}) }),
		pb(0.14, "warning.escalated", 0.2),
		ps(0.12, "warning.oncall_team", []string{"platform", "payments", "identity", "fulfillment", "search", "sre"}),
		pb(0.10, "warning.acknowledged", 0.4),
		// ---- rare ----
		pfn(0.07, "warning.related_incident", func() string { return fmt.Sprintf("INC-%d", randInt(10000, 99999)) }),
		pb(0.05, "warning.customer_impact", 0.3),
		pi(0.04, "warning.affected_customers_estimate", 0, 1000000),
		pf(0.03, "warning.slo_burn_rate", 10),
		pf(0.02, "warning.error_budget_remaining_percent", 100),
		pb(0.01, "warning.page_sent", 0.2),
		pfn(0.008, "warning.page_recipient", func() string { return fmt.Sprintf("oncall-%s@company.com", pickS([]string{"platform", "payments", "sre", "identity"})) }),
		pb(0.005, "warning.slo_breached", 0.05),
	}
}

func debugPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "debug.component", []string{"http_client", "db_pool", "cache_client", "grpc_client", "queue_consumer", "scheduler", "middleware", "serializer", "dns_resolver", "connection_manager", "circuit_breaker", "rate_limiter", "config_loader", "health_checker"}),
		ps(0.97, "debug.operation", []string{"serialize", "deserialize", "connect", "disconnect", "retry", "circuit_break", "rate_check", "auth_check", "dns_lookup", "tls_handshake", "pool_checkout", "pool_return", "config_read", "feature_eval"}),
		ps(0.95, "debug.context", []string{"connection acquired from pool", "request headers validated", "payload deserialized successfully", "middleware chain executed", "circuit breaker state: half-open", "retry attempt with exponential backoff", "feature flag evaluated", "config value resolved from environment", "DNS cache hit", "TLS session resumed", "connection keepalive sent", "idle connection reaped", "request queued for processing", "response streaming started"}),
		// ---- common ----
		pi(0.82, "debug.elapsed_us", 1, 1000000),
		pi(0.80, "thread.id", 1, 500),
		pfn(0.78, "thread.name", func() string { return fmt.Sprintf("worker-%d", randInt(1, 64)) }),
		ps(0.75, "code.function", []string{"handleRequest", "processMessage", "executeQuery", "fetchResource", "validateInput", "transformResponse", "dispatchEvent", "resolveConfig", "checkHealth", "routeRequest", "authenticate", "authorize"}),
		ps(0.72, "code.filepath", []string{"src/handler.go", "src/main/java/Service.java", "src/service.py", "src/controller.ts", "internal/middleware/auth.go", "pkg/client/http.go", "lib/cache/redis.rb"}),
		pi(0.70, "code.lineno", 1, 2000),
		ps(0.68, "code.namespace", []string{"com.example.service", "internal.handler", "pkg.middleware", "src.controllers", "lib.clients", "core.engine", "infra.adapters"}),
		// ---- moderate ----
		pfn(0.55, "debug.trace_id", func() string { return randomHex(32) }),
		pfn(0.52, "debug.span_id", func() string { return randomHex(16) }),
		pfn(0.50, "debug.parent_span_id", func() string { return randomHex(16) }),
		ps(0.48, "debug.sampler.type", samplerTypes),
		pf(0.45, "debug.sampler.param", 1),
		pi(0.42, "debug.goroutine_id", 1, 100000),
		pi(0.40, "debug.alloc_bytes", 0, 104857600),
		pi(0.38, "debug.gc_count_since_start", 0, 10000),
		pf(0.35, "debug.cpu_time_us", 1000000),
		pfn(0.33, "debug.memory_snapshot", func() string { return fmt.Sprintf("heap=%dMB stack=%dMB", randInt(50, 4000), randInt(1, 100)) }),
		// ---- uncommon ----
		ps(0.25, "debug.log_level_override", logLevels),
		pb(0.22, "debug.verbose_mode", 0.1),
		ps(0.20, "debug.feature_flag.name", featureFlags),
		pb(0.18, "debug.feature_flag.enabled", 0.7),
		ps(0.16, "debug.feature_flag.variant", abTestGroups),
		pfn(0.14, "debug.config.key", func() string { return pickS([]string{"max_connections", "timeout_ms", "retry_count", "batch_size", "cache_ttl", "rate_limit", "circuit_threshold"}) }),
		pfn(0.12, "debug.config.value", func() string { return fmt.Sprintf("%d", randInt(1, 10000)) }),
		pfn(0.10, "debug.config.source", func() string { return pickS([]string{"env", "file", "consul", "vault", "configmap", "ssm"}) }),
		// ---- rare ----
		pb(0.07, "debug.profiling_enabled", 0.05),
		pfn(0.05, "debug.pprof_url", func() string { return fmt.Sprintf("http://localhost:%d/debug/pprof", randInt(6060, 6070)) }),
		pb(0.04, "debug.race_detector_enabled", 0.01),
		ps(0.03, "debug.build_tags", []string{"debug", "trace", "profile", "integration", "e2e"}),
		pfn(0.02, "debug.git_commit", func() string { return randomHex(40) }),
		pfn(0.01, "debug.build_time", func() string { return fmt.Sprintf("2026-03-%02dT%02d:%02d:00Z", randInt(1, 31), randInt(0, 23), randInt(0, 59)) }),
		pb(0.008, "debug.dlv_attached", 0.01),
		pb(0.005, "debug.core_dump_on_crash", 0.5),
	}
}

func businessPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "business.event", bizEvents),
		pfnKV(0.97, func() log.KeyValue { return log.String("business.order_id", fmt.Sprintf("ord_%d", randInt(1000000, 9999999))) }),
		pfnKV(0.95, func() log.KeyValue { return log.String("business.customer_id", fmt.Sprintf("cust_%d", randInt(100000, 999999))) }),
		pi(0.93, "business.amount_cents", 1, 5000000),
		ps(0.90, "business.currency", currencies),
		// ---- common ----
		ps(0.82, "business.payment_method", payMethods),
		pi(0.80, "business.item_count", 1, 50),
		ps(0.78, "business.category", categories),
		ps(0.75, "business.channel", channels),
		ps(0.72, "business.shipping_method", []string{"standard", "express", "next_day", "pickup", "same_day", "freight"}),
		pb(0.70, "business.is_first_purchase", 0.15),
		ps(0.68, "business.discount_code", []string{"SAVE10", "WELCOME20", "FLASH50", "VIP15", "HOLIDAY30", "LOYALTY25", "FREESHIP", "BUNDLE15", ""}),
		// ---- moderate ----
		pf(0.55, "business.discount_percent", 50),
		pi(0.52, "business.discount_amount_cents", 0, 500000),
		pi(0.50, "business.tax_amount_cents", 0, 500000),
		pi(0.48, "business.shipping_amount_cents", 0, 50000),
		ps(0.45, "business.payment_status", []string{"pending", "authorized", "captured", "refunded", "partially_refunded", "voided", "failed", "disputed"}),
		pfn(0.42, "business.transaction_id", func() string { return fmt.Sprintf("txn_%s", randomHex(12)) }),
		ps(0.40, "business.customer_segment", []string{"new", "returning", "vip", "churned", "at_risk", "loyal", "high_value"}),
		pi(0.38, "business.customer_lifetime_value_cents", 0, 50000000),
		pi(0.35, "business.customer_order_count", 1, 500),
		ps(0.33, "business.fulfillment_status", []string{"unfulfilled", "partially_fulfilled", "fulfilled", "returned", "cancelled"}),
		// ---- uncommon ----
		pfn(0.25, "business.warehouse_id", func() string { return fmt.Sprintf("wh_%s_%d", pickS([]string{"east", "west", "central", "eu"}), randInt(1, 10)) }),
		pfn(0.22, "business.carrier", func() string { return pickS([]string{"UPS", "FedEx", "USPS", "DHL", "Amazon", "OnTrac", "LaserShip"}) }),
		pfn(0.20, "business.tracking_number", func() string { return fmt.Sprintf("1Z%s", randomHex(16)) }),
		pb(0.18, "business.is_gift", 0.08),
		ps(0.16, "business.gift_wrap_type", []string{"none", "standard", "premium", "eco"}),
		ps(0.14, "business.subscription.plan", []string{"basic", "pro", "enterprise", "team", "starter"}),
		ps(0.12, "business.subscription.interval", []string{"monthly", "quarterly", "annual"}),
		pb(0.10, "business.subscription.is_trial", 0.2),
		// ---- rare ----
		pb(0.07, "business.fraud_flag", 0.02),
		pf(0.05, "business.fraud_score", 1),
		ps(0.04, "business.fraud_reason", []string{"velocity_check", "geo_mismatch", "device_fingerprint", "card_testing", "account_takeover"}),
		pb(0.03, "business.chargeback_risk", 0.05),
		pfn(0.02, "business.affiliate_id", func() string { return fmt.Sprintf("aff_%d", randInt(1000, 9999)) }),
		pfn(0.01, "business.campaign_id", func() string { return fmt.Sprintf("camp_%s", randomHex(6)) }),
		ps(0.008, "business.referral_source", []string{"organic", "paid_search", "social", "email", "affiliate", "direct", "referral"}),
		pf(0.005, "business.conversion_value_usd", 10000),
	}
}

func mlPool() []ProbAttr {
	return []ProbAttr{
		ps(0.99, "ml.model.name", mlModels),
		pfnKV(0.97, func() log.KeyValue { return log.String("ml.model.version", fmt.Sprintf("v%d.%d", randInt(1, 10), randInt(0, 20))) }),
		ps(0.95, "ml.model.framework", mlFrameworks),
		pf(0.93, "ml.inference.latency_ms", 2000),
		// ---- common ----
		pi(0.82, "ml.inference.batch_size", 1, 128),
		pi(0.80, "ml.inference.input_tokens", 1, 8192),
		pi(0.78, "ml.inference.output_tokens", 1, 4096),
		pf(0.75, "ml.inference.confidence", 1),
		pf(0.72, "ml.inference.gpu_utilization", 1),
		pi(0.70, "ml.inference.gpu_memory_used_mb", 100, 80000),
		pi(0.68, "ml.feature.count", 1, 2000),
		pb(0.65, "ml.feature.cache_hit", 0.7),
		pfnKV(0.62, func() log.KeyValue { return log.String("ml.experiment.id", fmt.Sprintf("exp_%d", randInt(1000, 99999))) }),
		// ---- moderate ----
		ps(0.55, "ml.model.device", []string{"cpu", "gpu:0", "gpu:1", "gpu:2", "gpu:3", "tpu:0", "neuron:0"}),
		ps(0.52, "ml.model.precision", []string{"fp32", "fp16", "bf16", "int8", "int4"}),
		pi(0.50, "ml.model.parameters_millions", 1, 70000),
		pf(0.48, "ml.inference.preprocessing_ms", 100),
		pf(0.45, "ml.inference.postprocessing_ms", 100),
		pf(0.42, "ml.inference.queue_time_ms", 5000),
		pi(0.40, "ml.inference.input_size_bytes", 10, 10485760),
		pi(0.38, "ml.inference.output_size_bytes", 10, 1048576),
		ps(0.35, "ml.serving.runtime", []string{"triton", "tgi", "vllm", "torchserve", "tensorflow_serving", "onnx_runtime", "sagemaker"}),
		pi(0.33, "ml.serving.max_batch_size", 1, 256),
		// ---- uncommon ----
		pi(0.25, "ml.serving.queue_depth", 0, 1000),
		pf(0.22, "ml.serving.throughput_rps", 10000),
		pf(0.20, "ml.model.accuracy", 1),
		pf(0.18, "ml.model.f1_score", 1),
		pf(0.16, "ml.model.auc_roc", 1),
		pi(0.14, "ml.feature.missing_count", 0, 100),
		pf(0.12, "ml.feature.staleness_seconds", 86400),
		ps(0.10, "ml.ab_test.variant", abTestGroups),
		// ---- rare ----
		pf(0.07, "ml.model.drift_score", 1),
		pb(0.05, "ml.model.drift_detected", 0.1),
		pfn(0.04, "ml.model.training_run_id", func() string { return fmt.Sprintf("run_%s", randomHex(8)) }),
		pfn(0.03, "ml.model.registry_path", func() string { return fmt.Sprintf("s3://ml-models/%s/v%d", pickS(mlModels), randInt(1, 50)) }),
		pf(0.02, "ml.gpu.temperature_celsius", 100),
		pb(0.01, "ml.gpu.thermal_throttling", 0.05),
		pf(0.008, "ml.gpu.power_draw_watts", 400),
		pb(0.005, "ml.model.fallback_used", 0.02),
	}
}

// globalPool returns attributes that can appear on ANY log, regardless of template.
// These represent cross-cutting concerns.
func globalPool() []ProbAttr {
	return []ProbAttr{
		// ---- service mesh / networking (moderate-to-rare) ----
		ps(0.35, "net.transport", networkProtos),
		pfn(0.30, "net.sock.peer.addr", randomIP),
		pi(0.28, "net.sock.peer.port", 1024, 65535),
		ps(0.25, "service_mesh.proxy", []string{"envoy", "linkerd", "istio", "consul_connect", "nginx"}),
		ps(0.22, "service_mesh.action", svcMeshActions),
		pfn(0.20, "service_mesh.request_id", randomUUID),
		pf(0.18, "service_mesh.upstream_latency_ms", 5000),
		ps(0.15, "dns.question.name", []string{"user-svc.internal", "db-primary.internal", "cache-main.internal", "kafka-broker-1.internal", "api.external.com"}),
		ps(0.12, "dns.question.type", dnsTypes),
		pf(0.10, "dns.lookup_duration_ms", 100),
		// ---- feature flags / experiments ----
		ps(0.20, "feature_flag.key", featureFlags),
		pb(0.20, "feature_flag.enabled", 0.7),
		ps(0.15, "feature_flag.variant", abTestGroups),
		pfn(0.12, "feature_flag.context_hash", func() string { return randomHex(8) }),
		ps(0.08, "feature_flag.provider", []string{"launchdarkly", "split", "flagsmith", "unleash", "internal"}),
		// ---- tracing context ----
		pfn(0.40, "trace.id", func() string { return randomHex(32) }),
		pfn(0.40, "span.id", func() string { return randomHex(16) }),
		pfn(0.30, "parent.span.id", func() string { return randomHex(16) }),
		ps(0.25, "span.kind", []string{"server", "client", "internal", "producer", "consumer"}),
		pb(0.20, "trace.sampled", 0.1),
		// ---- security context ----
		pfn(0.15, "security.principal", func() string { return fmt.Sprintf("usr_%d", randInt(100000, 999999)) }),
		ps(0.12, "security.protocol", []string{"TLSv1.2", "TLSv1.3", "mTLS", "none"}),
		pb(0.10, "security.mtls_verified", 0.8),
		pfn(0.08, "security.certificate.serial", func() string { return randomHex(20) }),
		pfn(0.06, "security.certificate.issuer", func() string { return pickS([]string{"Let's Encrypt", "DigiCert", "Internal CA", "Vault PKI"}) }),
		pi(0.05, "security.certificate.days_until_expiry", 0, 365),
		// ---- deployment context ----
		ps(0.18, "deployment.canary.weight", []string{"0", "1", "5", "10", "25", "50", "100"}),
		ps(0.15, "deployment.strategy", []string{"rolling", "blue_green", "canary", "recreate", "a_b"}),
		pfn(0.12, "deployment.rollout_id", func() string { return fmt.Sprintf("rollout-%s", randomHex(6)) }),
		pb(0.08, "deployment.is_canary", 0.1),
		pfn(0.06, "deployment.previous_version", func() string { return fmt.Sprintf("1.%d.%d", randInt(0, 30), randInt(0, 100)) }),
		// ---- cost / billing metadata ----
		pfn(0.10, "cost.estimated_usd", func() string { return fmt.Sprintf("%.6f", rand.Float64()*0.01) }),
		ps(0.08, "cost.tier", []string{"free", "standard", "premium", "enterprise"}),
		pfn(0.06, "cost.budget_id", func() string { return fmt.Sprintf("budget-%s", randomHex(4)) }),
		// ---- compliance / audit ----
		ps(0.08, "compliance.data_classification", []string{"public", "internal", "confidential", "restricted", "pii", "phi"}),
		pb(0.06, "compliance.pii_accessed", 0.1),
		ps(0.05, "compliance.regulation", []string{"GDPR", "CCPA", "SOC2", "HIPAA", "PCI-DSS", "SOX"}),
		pfn(0.04, "compliance.audit_event_id", randomUUID),
		pb(0.03, "compliance.consent_verified", 0.9),
		// ---- rare cross-cutting ----
		pfn(0.02, "baggage.tenant_id", func() string { return fmt.Sprintf("tenant_%d", randInt(1, 500)) }),
		pfn(0.02, "baggage.request_priority", func() string { return pickS([]string{"low", "normal", "high", "critical"}) }),
		pfn(0.01, "baggage.synthetic_test_id", func() string { return fmt.Sprintf("synth_%s", randomHex(8)) }),
		pb(0.01, "baggage.is_internal", 0.2),
		pfn(0.008, "baggage.experiment_id", func() string { return fmt.Sprintf("exp_%d", randInt(1, 10000)) }),
		pfn(0.005, "custom.label.team_override", func() string { return pickS([]string{"platform", "payments", "identity", "catalog", "search"}) }),
		pfn(0.003, "custom.label.incident_id", func() string { return fmt.Sprintf("INC-%d", randInt(10000, 99999)) }),
		pb(0.002, "custom.label.is_postmortem_trace", 1.0),
	}
}

var (
	globalAttrs []ProbAttr

	logTemplates []LogTemplate
)

func init() {
	globalAttrs = globalPool()

	logTemplates = []LogTemplate{
		{log.SeverityInfo, "INFO", "HTTP request completed", httpRequestPool(), 30, []string{"api-gateway", "auth-service", "user-service", "product-catalog", "search-api", "order-service", "cart-service", "checkout-orchestrator"}},
		{log.SeverityInfo, "INFO", "Database query executed", dbQueryPool(), 20, []string{"user-service", "product-catalog", "order-service", "payment-processor", "billing-service", "inventory-service", "review-service", "cart-service"}},
		{log.SeverityInfo, "INFO", "Cache operation completed", cachePool(), 15, []string{"cache-proxy", "user-service", "product-catalog", "session-manager", "search-api", "cart-service", "feature-flags", "rate-limiter"}},
		{log.SeverityInfo, "INFO", "Message published to queue", messageQueuePool(), 10, []string{"event-bus", "order-service", "payment-processor", "notification-router", "analytics-collector", "clickstream-processor", "inventory-service"}},
		{log.SeverityInfo, "INFO", "gRPC call completed", grpcPool(), 12, []string{"api-gateway", "auth-service", "user-service", "payment-gateway", "search-api", "order-service", "ml-model-server", "recommendation-engine"}},
		{log.SeverityInfo, "INFO", "Application lifecycle event", lifecyclePool(), 2, []string{"*"}},
		{log.SeverityInfo, "INFO", "Authentication event processed", authPool(), 8, []string{"auth-service", "session-manager", "token-service", "api-gateway"}},
		{log.SeverityError, "ERROR", "Error occurred during request processing", errorPool(), 5, []string{"*"}},
		{log.SeverityWarn, "WARN", "Performance degradation detected", warningPool(), 7, []string{"*"}},
		{log.SeverityDebug, "DEBUG", "Debug trace information", debugPool(), 5, []string{"*"}},
		{log.SeverityInfo, "INFO", "Business metric recorded", businessPool(), 6, []string{"order-service", "payment-processor", "checkout-orchestrator", "analytics-collector", "billing-service", "cart-service"}},
		{log.SeverityInfo, "INFO", "ML inference completed", mlPool(), 4, []string{"ml-model-server", "ml-feature-store", "recommendation-engine", "fraud-detector", "search-ranker"}},
	}
}

func PickWeightedTemplate(serviceName string) *LogTemplate {
	var applicable []*LogTemplate
	totalWeight := 0
	for i := range logTemplates {
		t := &logTemplates[i]
		for _, p := range t.ServicePatterns {
			if p == "*" || p == serviceName {
				applicable = append(applicable, t)
				totalWeight += t.Weight
				break
			}
		}
	}
	r := rand.Intn(totalWeight)
	for _, t := range applicable {
		r -= t.Weight
		if r < 0 {
			return t
		}
	}
	return applicable[len(applicable)-1]
}
