package metricgen

import (
	"fmt"
	"sync/atomic"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

// resourcePool derives and caches OTLP Resource protos for the simulated
// fleet. Resource attributes are deterministic functions of (index,
// generation); the generation changes when churn is enabled and a resource
// "restarts" (new pod name / instance id, so every series it owns becomes a
// new series).
//
// The cache is shared by all workers. Each slot keeps the two most recent
// generations because workers sweep independently and may briefly disagree on
// the current generation; a slower worker must still see the older resource
// identity or its points would be attributed to the wrong (new) series.
type resourcePool struct {
	cfg      Config
	services []string
	slots    []atomic.Pointer[resourceSlot]
}

type resourceSlot struct {
	gen  uint64
	res  *resourcepb.Resource
	prev *resourceGen
}

type resourceGen struct {
	gen uint64
	res *resourcepb.Resource
}

var regions = []string{"us-east-1", "us-west-2", "eu-west-1", "eu-central-1", "ap-southeast-1", "sa-east-1"}
var namespaces = []string{"prod", "staging", "canary"}
var sdkLanguages = []string{"go", "java", "python", "nodejs", "dotnet", "rust"}

func newResourcePool(cfg Config) *resourcePool {
	return &resourcePool{
		cfg:      cfg,
		services: serviceNames(cfg.Services),
		slots:    make([]atomic.Pointer[resourceSlot], cfg.Resources),
	}
}

// serviceName returns the service a resource belongs to (stable across
// generations).
func (rp *resourcePool) serviceName(idx int) string {
	return rp.services[idx%len(rp.services)]
}

// get returns the Resource proto for (idx, gen), building and caching it on
// first use. Lock-free: concurrent builders may race, losing at most a
// duplicate allocation.
func (rp *resourcePool) get(idx int, gen uint64) *resourcepb.Resource {
	slot := rp.slots[idx].Load()
	if slot != nil {
		if slot.gen == gen {
			return slot.res
		}
		if slot.prev != nil && slot.prev.gen == gen {
			return slot.prev.res
		}
	}

	res := rp.build(idx, gen)
	next := &resourceSlot{gen: gen, res: res}
	if slot != nil && slot.gen < gen {
		next.prev = &resourceGen{gen: slot.gen, res: slot.res}
	}
	// Only advance the cache forward; a stale worker reading an old generation
	// just rebuilds without publishing.
	if slot == nil || slot.gen < gen {
		rp.slots[idx].CompareAndSwap(slot, next)
	}
	return res
}

// build derives the full resource attribute set for (idx, gen). Everything is
// stable per (idx, gen); only the pod name and instance id change across
// generations, mirroring a pod restart under the same service/node placement.
func (rp *resourcePool) build(idx int, gen uint64) *resourcepb.Resource {
	service := rp.serviceName(idx)
	h := mix2(uint64(idx), 0x7265736f75726365) // stable per resource
	gh := mix3(uint64(idx), gen, 0x706f64)     // varies per generation

	namespace := namespaces[h%uint64(len(namespaces))]
	region := regions[(h>>8)%uint64(len(regions))]
	zone := fmt.Sprintf("%s%c", region, 'a'+byte((h>>16)%3))
	node := fmt.Sprintf("node-%s-%d", region, uint64(idx)%max(1, uint64(rp.cfg.Resources/10)))
	pod := fmt.Sprintf("%s-%s-%s", service, hexBytes(h, 10), hexBytes(gh, 5))

	attrs := []*commonpb.KeyValue{
		strAttr("service.name", service),
		strAttr("service.namespace", namespace),
		strAttr("service.instance.id", hexBytes(splitmix64(gh), 8)+"-"+hexBytes(gh>>32, 4)),
		strAttr("k8s.pod.name", pod),
		strAttr("k8s.namespace.name", namespace),
		strAttr("k8s.node.name", node),
		strAttr("host.name", node),
		strAttr("cloud.region", region),
		strAttr("cloud.availability_zone", zone),
		strAttr("os.type", "linux"),
		strAttr("telemetry.sdk.name", "opentelemetry"),
		strAttr("telemetry.sdk.language", sdkLanguages[(h>>24)%uint64(len(sdkLanguages))]),
	}
	return &resourcepb.Resource{Attributes: attrs}
}

func strAttr(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}
