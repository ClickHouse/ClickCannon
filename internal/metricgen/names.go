package metricgen

import (
	"fmt"
	"math/rand/v2"
)

// Word lists behind the metric/service name tables: names are "<adjective>_<adjective>_<noun>" with two distinct
// adjectives, derived from fixed seeds (not app.seed), so the same index always maps to the same name across runs.
var adjectives = []string{
	"swift", "vibrant", "ancient", "shadowy", "silent", "crimson", "frozen", "golden", "hidden", "cosmic",
	"mystic", "rugged", "luminous", "spectral", "rusty", "whispering", "emerald", "furious", "serene", "boreal",
	"celestial", "obsidian", "azure", "primal", "endless", "infinite", "gilded", "forgotten", "arcane", "stellar",
	"radiant", "somber", "twinkling", "murmuring", "shimmering", "blazing", "velvet", "frosty", "echoing", "timeless",
}

var nouns = []string{
	"horizon", "phoenix", "glacier", "canyon", "voyager", "nebula", "tempest", "cavern", "citadel", "summit",
	"eclipse", "oasis", "phantom", "echo", "oracle", "monolith", "vortex", "beacon", "odyssey", "sanctuary",
	"tundra", "comet", "spire", "crag", "mirage", "sentinel", "current", "threshold", "bastion", "relic",
	"cascade", "abyss", "fable", "matrix", "prophet", "mariner", "enclave", "zenith", "pinnacle", "artifact",
}

// MaxMetricCount bounds metric_count. The phrase space is 40*39*40 = 62,400;
// rejection sampling degrades near saturation, so cap well below it.
const MaxMetricCount = 50000

// nameTableSeed1/2 are fixed so the lookup table never changes. Treat a change
// to these (or the word lists) as a breaking change to every saved workload.
const (
	nameTableSeed1 = 0x636c69636b63616e // "clickcan"
	nameTableSeed2 = 0x6d65747269637331 // "metrics1"
)

// metricNames returns the first n entries of the metric name lookup table.
func metricNames(n int) []string {
	rng := rand.New(rand.NewPCG(nameTableSeed1, nameTableSeed2))
	seen := make(map[string]struct{}, n)
	names := make([]string, 0, n)
	for len(names) < n {
		adj1 := adjectives[rng.IntN(len(adjectives))]
		adj2 := adjectives[rng.IntN(len(adjectives))]
		if adj1 == adj2 {
			continue
		}
		name := adj1 + "_" + adj2 + "_" + nouns[rng.IntN(len(nouns))]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

// serviceNames returns n stable service names ("adjective-noun"), from the same
// fixed-seed scheme as the metric name table. Suffixes disambiguate collisions
// so any n is safe.
func serviceNames(n int) []string {
	rng := rand.New(rand.NewPCG(nameTableSeed2, nameTableSeed1))
	seen := make(map[string]struct{}, n)
	names := make([]string, 0, n)
	for len(names) < n {
		name := adjectives[rng.IntN(len(adjectives))] + "-" + nouns[rng.IntN(len(nouns))]
		if _, ok := seen[name]; ok {
			name = fmt.Sprintf("%s-%d", name, len(names))
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
