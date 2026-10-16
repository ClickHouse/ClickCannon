package metricgen

import (
	"math"
	"time"

	"clickcannon/internal/otlpsize"
)

const sizeSeriesPerMetric = 8

type attrStats struct {
	sum, weight float64
	min, max    int
}

func (s *attrStats) add(bytes int, weight float64) {
	if s.weight == 0 || bytes < s.min {
		s.min = bytes
	}
	s.max = max(s.max, bytes)
	s.sum += float64(bytes) * weight
	s.weight += weight
}

func (s attrStats) size() otlpsize.AttrSize {
	return otlpsize.AttrSize{Expected: int(math.Round(s.sum / s.weight)), Min: s.min, Max: s.max}
}

type levelStats struct {
	resource, scope, signal attrStats
}

// SyntheticSize returns the attribute size of one data point per metric type. Each metric is weighted by its
// series count, estimated from evenly spaced series of the metric.
func SyntheticSize(cfg Config) ([]otlpsize.Breakdown, error) {
	p, err := newPlan(cfg.withDefaults(), "", time.Now().UTC())
	if err != nil {
		return nil, err
	}

	var stats [typeSummary + 1]levelStats
	for _, def := range p.metrics {
		st := &stats[def.typ]
		scopeBytes := otlpsize.AttrBytes(p.scopes[def.scopeIdx].scope.Attributes...)
		n := min(def.cardinality, sizeSeriesPerMetric)
		weight := float64(def.cardinality) / float64(n)
		for i := range n {
			s := makeSeriesRef(def, i*def.cardinality/n, p.cfg.Resources)
			st.resource.add(otlpsize.AttrBytes(p.resources.build(s.resourceIdx, 0).Attributes...), weight)
			st.scope.add(scopeBytes, weight)
			st.signal.add(otlpsize.AttrBytes(s.appendAttrs(nil)...), weight)
		}
	}

	var out []otlpsize.Breakdown
	for typ, st := range stats {
		if st.resource.weight == 0 {
			continue
		}
		out = append(out, otlpsize.Breakdown{
			Profile:       "metricgen",
			Signal:        "metrics/" + metricType(typ).String(),
			ResourceAttrs: st.resource.size(),
			ScopeAttrs:    st.scope.size(),
			SignalAttrs:   st.signal.size(),
		})
	}
	return out, nil
}
