package otel

import (
	"fmt"
	"math"

	"clickcannon/internal/block"
	"clickcannon/internal/generate"
	"clickcannon/internal/otlpsize"
)

const attrSizeSamples = 100

// SyntheticSize returns the attribute size of one log record or span of the generate profile.
func SyntheticSize(dataType string, cfg generate.Config, seed string) (otlpsize.Breakdown, error) {
	name := cfg.Profile
	if name == "" {
		name = generate.DefaultProfile
	}
	profile, err := generate.GetProfile(name)
	if err != nil {
		return otlpsize.Breakdown{}, err
	}

	rng := generate.NewRng(seed, 0)
	bd := otlpsize.Breakdown{Profile: name, Signal: dataType, ResourceAttrs: attrSize(resourceAttrEntries(profile), rng)}
	switch dataType {
	case "logs":
		bd.ScopeAttrs = attrSize(profile.ScopeAttrs.Entries(), rng)
		bd.SignalAttrs = attrSize(profile.LogAttrs.Entries(), rng)
	case "traces":
		bd.SignalAttrs = attrSize(profile.SpanAttrs.Entries(), rng)
	default:
		return otlpsize.Breakdown{}, fmt.Errorf("otlp size does not support data_type %q", dataType)
	}
	return bd, nil
}

// resourceAttrEntries adds the service.name attribute that buildResource adds from the ServiceName column.
func resourceAttrEntries(profile *generate.Profile) []generate.MapEntryInfo {
	entries := profile.ResourceAttrs.Entries()
	for _, e := range entries {
		if e.Key == "service.name" && !e.RandomKey {
			return entries
		}
	}
	return append(entries, generate.MapEntryInfo{Key: "service.name", Prob: 1, ValueGen: profile.ServiceName})
}

func attrSize(entries []generate.MapEntryInfo, rng *generate.Rng) otlpsize.AttrSize {
	var size otlpsize.AttrSize
	var expected float64
	for _, e := range entries {
		if e.Prob <= 0 {
			continue
		}
		lo, hi, total := math.MaxInt, 0, 0
		for range attrSizeSamples {
			key := e.Key
			if e.RandomKey {
				key += rng.HexString(e.SuffixLen)
			}
			n := otlpsize.AttrBytes(attrsFromKV([]block.KV{{Key: key, Value: e.ValueGen.Generate(rng)}})...)
			lo, hi, total = min(lo, n), max(hi, n), total+n
		}
		expected += min(e.Prob, 1) * float64(total) / attrSizeSamples
		size.Max += hi
		if e.Prob >= 1 {
			size.Min += lo
		}
	}
	size.Expected = int(math.Round(expected))
	return size
}
