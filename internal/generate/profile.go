package generate

import (
	"fmt"
	"sort"
)

// Profile is the code-defined replacement for the old YAML profiles.
// It groups every per-column generator for both the logs and traces schemas.
// Fields that don't apply to the active data type are simply ignored.
//
// Build a profile with builder methods, e.g.
//
//	p := NewProfile().
//	    WithServiceName(Pool(V("api-gw", 10), V("db", 5))).
//	    WithBody(Pool(V("HTTP request completed", 30))).
//	    WithResourceAttrs(Map(
//	        K("cloud.provider", 1.0, Const("aws")),
//	        K("process.pid", 0.9, Int(1, 65535)),
//	    ))
//
// or just assign the fields directly. The builder is purely cosmetic — both styles
// produce the same generator objects.
type Profile struct {
	Name string

	ServiceName   Gen
	ScopeName     Gen
	ScopeVersion  Gen
	ResourceAttrs *MapGen
	ScopeAttrs    *MapGen

	// Logs-only
	SeverityText Gen
	Body         Gen
	LogAttrs     *MapGen

	// Traces-only
	SpanName      Gen
	SpanKind      Gen
	StatusCode    Gen
	StatusMessage Gen
	SpanAttrs     *MapGen

	// Profiles-only
	SampleType      Gen
	SampleUnit      Gen
	PeriodType      Gen
	PeriodUnit      Gen
	FunctionName    Gen
	FileName        Gen
	MappingFileName Gen
	ProfileAttrs    *MapGen
	SampleAttrs     *MapGen

	// Metrics-only
	MetricName        Gen
	MetricDescription Gen
	MetricUnit        Gen     // fallback unit when MetricName isn't in the unit map
	DataPointAttrs    *MapGen // per-datapoint Attributes column
}

// NewProfile returns an empty Profile. Use the With* builders or set fields directly.
func NewProfile(name string) *Profile { return &Profile{Name: name} }

func (p *Profile) WithServiceName(g Gen) *Profile       { p.ServiceName = g; return p }
func (p *Profile) WithScopeName(g Gen) *Profile         { p.ScopeName = g; return p }
func (p *Profile) WithScopeVersion(g Gen) *Profile      { p.ScopeVersion = g; return p }
func (p *Profile) WithResourceAttrs(m *MapGen) *Profile { p.ResourceAttrs = m; return p }
func (p *Profile) WithScopeAttrs(m *MapGen) *Profile    { p.ScopeAttrs = m; return p }

func (p *Profile) WithSeverityText(g Gen) *Profile { p.SeverityText = g; return p }
func (p *Profile) WithBody(g Gen) *Profile         { p.Body = g; return p }
func (p *Profile) WithLogAttrs(m *MapGen) *Profile { p.LogAttrs = m; return p }

func (p *Profile) WithSpanName(g Gen) *Profile      { p.SpanName = g; return p }
func (p *Profile) WithSpanKind(g Gen) *Profile      { p.SpanKind = g; return p }
func (p *Profile) WithStatusCode(g Gen) *Profile    { p.StatusCode = g; return p }
func (p *Profile) WithStatusMessage(g Gen) *Profile { p.StatusMessage = g; return p }
func (p *Profile) WithSpanAttrs(m *MapGen) *Profile { p.SpanAttrs = m; return p }

func (p *Profile) WithSampleType(g Gen) *Profile       { p.SampleType = g; return p }
func (p *Profile) WithSampleUnit(g Gen) *Profile       { p.SampleUnit = g; return p }
func (p *Profile) WithPeriodType(g Gen) *Profile       { p.PeriodType = g; return p }
func (p *Profile) WithPeriodUnit(g Gen) *Profile       { p.PeriodUnit = g; return p }
func (p *Profile) WithFunctionName(g Gen) *Profile     { p.FunctionName = g; return p }
func (p *Profile) WithFileName(g Gen) *Profile         { p.FileName = g; return p }
func (p *Profile) WithMappingFileName(g Gen) *Profile  { p.MappingFileName = g; return p }
func (p *Profile) WithProfileAttrs(m *MapGen) *Profile { p.ProfileAttrs = m; return p }
func (p *Profile) WithSampleAttrs(m *MapGen) *Profile  { p.SampleAttrs = m; return p }

func (p *Profile) WithMetricName(g Gen) *Profile         { p.MetricName = g; return p }
func (p *Profile) WithMetricDescription(g Gen) *Profile  { p.MetricDescription = g; return p }
func (p *Profile) WithMetricUnit(g Gen) *Profile         { p.MetricUnit = g; return p }
func (p *Profile) WithDataPointAttrs(m *MapGen) *Profile { p.DataPointAttrs = m; return p }

// applyDefaults fills any unset generators with safe default constants so the
// fillers never need to nil-check on the hot path.
func (p *Profile) applyDefaults() {
	if p.ServiceName == nil {
		p.ServiceName = Const("unknown")
	}
	if p.ScopeName == nil {
		p.ScopeName = Const("")
	}
	if p.ScopeVersion == nil {
		p.ScopeVersion = Const("")
	}
	if p.ResourceAttrs == nil {
		p.ResourceAttrs = Map()
	}
	if p.ScopeAttrs == nil {
		p.ScopeAttrs = Map()
	}
	if p.SeverityText == nil {
		p.SeverityText = Const("INFO")
	}
	if p.Body == nil {
		p.Body = Const("")
	}
	if p.LogAttrs == nil {
		p.LogAttrs = Map()
	}
	if p.SpanName == nil {
		p.SpanName = Const("unknown")
	}
	if p.SpanKind == nil {
		p.SpanKind = Const("SPAN_KIND_INTERNAL")
	}
	if p.StatusCode == nil {
		p.StatusCode = Const("STATUS_CODE_UNSET")
	}
	if p.StatusMessage == nil {
		p.StatusMessage = Const("")
	}
	if p.SpanAttrs == nil {
		p.SpanAttrs = Map()
	}
	if p.SampleType == nil {
		p.SampleType = Pool("cpu", "samples", "alloc_space", "inuse_space", "alloc_objects", "inuse_objects")
	}
	if p.SampleUnit == nil {
		p.SampleUnit = Pool("nanoseconds", "count", "bytes")
	}
	if p.PeriodType == nil {
		p.PeriodType = Const("cpu")
	}
	if p.PeriodUnit == nil {
		p.PeriodUnit = Const("nanoseconds")
	}
	if p.FunctionName == nil {
		p.FunctionName = AnyOf(
			Pool("main.main", "runtime.mallocgc", "runtime.gcBgMarkWorker", "runtime.systemstack", "runtime.futex", "runtime.memmove", "runtime.scanobject", "runtime.mapaccess2", "runtime.growslice", "net/http.(*conn).serve", "net/http.(*ServeMux).ServeHTTP", "sync.(*Mutex).Lock", "encoding/json.Marshal", "encoding/json.Unmarshal", "database/sql.(*DB).QueryContext", "google.golang.org/grpc.(*Server).processUnaryRPC"),
			RandStr(10).Prefix("app/internal/service."),
			RandStr(8).Prefix("app/internal/handler."),
			RandStr(10).Prefix("vendor/github.com/"),
		)
	}
	if p.FileName == nil {
		p.FileName = AnyOf(
			Pool("/usr/local/go/src/runtime/proc.go", "/usr/local/go/src/runtime/malloc.go", "/usr/local/go/src/net/http/server.go", "/usr/local/go/src/sync/mutex.go", "/usr/local/go/src/encoding/json/encode.go"),
			RandStr(8).Prefix("/app/internal/service/"),
			RandStr(8).Prefix("/app/internal/handler/"),
			RandStr(8).Prefix("/go/pkg/mod/github.com/"),
		)
	}
	if p.MappingFileName == nil {
		p.MappingFileName = Pool("/app/server", "/usr/lib/x86_64-linux-gnu/libc.so.6", "/usr/lib/x86_64-linux-gnu/libpthread.so.0", "/usr/local/go/pkg/tool/linux_amd64/link", "[vdso]", "[kernel.kallsyms]")
	}
	if p.ProfileAttrs == nil {
		p.ProfileAttrs = Map(
			K(0.95, "profiler.name", "otel-ebpf-profiler", "pyroscope", "parca", "async-profiler", "datadog-profiler"),
			K(0.9, "profiler.version", RandStr(5).Prefix("1.")),
			K(0.7, "process.pid", Int(1, 65535)),
			K(0.5, "runtime.name", "go", "OpenJDK", "CPython", "Node.js"),
		)
	}
	if p.SampleAttrs == nil {
		p.SampleAttrs = Map(
			K(0.8, "thread.name", "main", "worker", "gc", "http-handler", "grpc-worker", "scheduler", "io-poller"),
			K(0.8, "thread.id", Int(1, 4096)),
			K(0.3, "endpoint", "/api/v1/users", "/api/v1/orders", "/api/v1/search", "/api/v1/checkout", "/internal/health"),
			K(0.15, "tenant.id", Int(1000, 9999).Prefix("tenant-")),
		)
	}
	if p.MetricName == nil {
		p.MetricName = Pool(
			"http.server.duration",
			"http.server.request.size",
			"http.server.response.size",
			"http.client.duration",
			"rpc.server.duration",
			"db.client.connections.usage",
			"system.cpu.utilization",
			"system.memory.usage",
			"system.disk.io",
			"system.network.io",
			"process.runtime.jvm.memory.usage",
			"process.runtime.go.goroutines",
			"process.runtime.go.mem.heap_alloc",
			"kafka.consumer.records_consumed_total",
			"queue.size",
			"app.orders.processed",
		)
	}
	if p.MetricDescription == nil {
		p.MetricDescription = Const("")
	}
	if p.MetricUnit == nil {
		p.MetricUnit = Pool("1", "ms", "s", "By", "{count}")
	}
	if p.DataPointAttrs == nil {
		p.DataPointAttrs = Map(
			K(0.9, "http.method", "GET", "GET", "GET", "POST", "PUT", "DELETE"),
			K(0.9, "http.status_code", "200", "200", "200", "201", "204", "400", "404", "500"),
			K(0.7, "http.route", "/api/v1/users", "/api/v1/orders", "/api/v1/search", "/api/v1/checkout", "/internal/health"),
			K(0.5, "net.protocol.name", "http", "grpc"),
			K(0.3, "db.system", "postgresql", "mysql", "clickhouse", "redis"),
			K(0.15, "tenant.id", Int(1000, 9999).Prefix("tenant-")),
		)
	}
}

// =============================================================================
// Registry: code-defined profiles registered at init() time.
// =============================================================================

var profileRegistry = map[string]func() *Profile{}

// RegisterProfile makes a profile available under the given name. Call from init().
// Panics on duplicate name (caught at process startup).
func RegisterProfile(name string, build func() *Profile) {
	if _, exists := profileRegistry[name]; exists {
		panic("generate: profile already registered: " + name)
	}
	profileRegistry[name] = build
}

// GetProfile builds and returns the profile registered under name.
// Each call returns a fresh instance so generators are not shared across workers
// (most are stateless after construction, but this keeps things simple).
func GetProfile(name string) (*Profile, error) {
	build, ok := profileRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q (registered: %s)", name, registeredProfileNames())
	}
	p := build()
	p.applyDefaults()
	return p, nil
}

// ListProfiles returns the registered profile names, sorted.
func ListProfiles() []string {
	names := make([]string, 0, len(profileRegistry))
	for n := range profileRegistry {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func registeredProfileNames() string {
	names := ListProfiles()
	s := ""
	for i, n := range names {
		if i > 0 {
			s += ", "
		}
		s += n
	}
	return s
}
