package generate

import (
	"fmt"
	"strconv"
)

// Gen produces a single string value per row from a seeded RNG.
type Gen interface {
	Generate(rng *Rng) string
}

// genFunc adapts a func.
type genFunc func(*Rng) string

func (f genFunc) Generate(rng *Rng) string { return f(rng) }

// Func wraps an arbitrary function as a Gen.
func Func(f func(*Rng) string) Gen { return genFunc(f) }

// =============================================================================
// Const — always emits the same string.
// =============================================================================

type constGen struct{ v string }

func (g *constGen) Generate(_ *Rng) string { return g.v }

func Const(v string) Gen { return &constGen{v: v} }

// =============================================================================
// Pool — uniform random pick over a string list.
// The slice is borrowed (not copied); callers must not mutate it later.
// =============================================================================

type poolGen struct{ vals []string }

func (g *poolGen) Generate(rng *Rng) string {
	return g.vals[rng.IntN(len(g.vals))]
}

func Pool(vals ...string) Gen {
	switch len(vals) {
	case 0:
		return Const("")
	case 1:
		return Const(vals[0])
	}
	return &poolGen{vals: vals}
}

// =============================================================================
// AnyOf — uniform pick over Gens. Useful when a column mixes a plain pool with
// a random-suffix generator (e.g. SpanName has fixed routes plus "db.query"+random).
// =============================================================================

type anyOfGen struct{ gens []Gen }

func (g *anyOfGen) Generate(rng *Rng) string { return g.gens[rng.IntN(len(g.gens))].Generate(rng) }

func AnyOf(gens ...Gen) Gen {
	switch len(gens) {
	case 0:
		return Const("")
	case 1:
		return gens[0]
	}
	return &anyOfGen{gens: gens}
}

// =============================================================================
// RandStr — random alphanumeric of fixed length, optional prefix.
// =============================================================================

type randStrGen struct {
	prefix string
	length int
}

func (g *randStrGen) Generate(rng *Rng) string {
	if g.prefix == "" {
		return rng.AlphaNum(g.length)
	}
	return g.prefix + rng.AlphaNum(g.length)
}

func RandStr(length int) *randStrGen              { return &randStrGen{length: length} }
func (g *randStrGen) Prefix(p string) *randStrGen { g.prefix = p; return g }

// =============================================================================
// Hex — random lowercase hex of fixed length, optional prefix.
// =============================================================================

type hexGen struct {
	prefix string
	length int
}

func (g *hexGen) Generate(rng *Rng) string {
	if g.prefix == "" {
		return rng.HexString(g.length)
	}
	return g.prefix + rng.HexString(g.length)
}

func Hex(length int) *hexGen              { return &hexGen{length: length} }
func (g *hexGen) Prefix(p string) *hexGen { g.prefix = p; return g }

// =============================================================================
// UUID
// =============================================================================

type uuidGen struct{}

func (g *uuidGen) Generate(rng *Rng) string { return rng.UUID() }

func UUID() Gen { return &uuidGen{} }

// =============================================================================
// IP — random IPv4, formatted as dotted-quad by default, uint32 or hex on request.
// =============================================================================

type ipFormat int

const (
	ipFmtDotted ipFormat = iota
	ipFmtU32
	ipFmtHex
)

type ipGen struct{ format ipFormat }

func (g *ipGen) Generate(rng *Rng) string {
	v := rng.Uint32()
	switch g.format {
	case ipFmtU32:
		return strconv.FormatUint(uint64(v), 10)
	case ipFmtHex:
		return formatHex32(v)
	default:
		return formatIPv4(v)
	}
}

func IP() *ipGen               { return &ipGen{} }
func (g *ipGen) AsU32() *ipGen { g.format = ipFmtU32; return g }
func (g *ipGen) AsHex() *ipGen { g.format = ipFmtHex; return g }

const hexDigits = "0123456789abcdef"

func formatHex32(v uint32) string {
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[7-i] = hexDigits[(v>>(i*4))&0xF]
	}
	return string(buf[:])
}

func formatIPv4(v uint32) string {
	var buf [15]byte
	n := 0
	for shift := 24; shift >= 0; shift -= 8 {
		oct := byte(v >> uint(shift))
		switch {
		case oct >= 100:
			buf[n] = '0' + oct/100
			n++
			buf[n] = '0' + (oct/10)%10
			n++
			buf[n] = '0' + oct%10
			n++
		case oct >= 10:
			buf[n] = '0' + oct/10
			n++
			buf[n] = '0' + oct%10
			n++
		default:
			buf[n] = '0' + oct
			n++
		}
		if shift > 0 {
			buf[n] = '.'
			n++
		}
	}
	return string(buf[:n])
}

// =============================================================================
// Int / Float / Bool
// =============================================================================

type intGen struct {
	prefix   string
	min, max int
}

func (g *intGen) Generate(rng *Rng) string {
	var v int
	if g.max > g.min {
		v = g.min + rng.IntN(g.max-g.min+1)
	} else {
		v = g.min
	}
	if g.prefix == "" {
		return strconv.Itoa(v)
	}
	return g.prefix + strconv.Itoa(v)
}

func Int(min, max int) *intGen            { return &intGen{min: min, max: max} }
func (g *intGen) Prefix(p string) *intGen { g.prefix = p; return g }

type floatGen struct {
	prefix   string
	max      float64
	decimals int
}

func (g *floatGen) Generate(rng *Rng) string {
	v := rng.Float64() * g.max
	if g.prefix == "" {
		return strconv.FormatFloat(v, 'f', g.decimals, 64)
	}
	return g.prefix + strconv.FormatFloat(v, 'f', g.decimals, 64)
}

func Float(max float64) *floatGen             { return &floatGen{max: max, decimals: 2} }
func (g *floatGen) Precision(n int) *floatGen { g.decimals = n; return g }
func (g *floatGen) Prefix(p string) *floatGen { g.prefix = p; return g }

type boolGen struct{ trueProb float64 }

func (g *boolGen) Generate(rng *Rng) string {
	if rng.Float64() < g.trueProb {
		return "true"
	}
	return "false"
}

func Bool(trueProb float64) Gen { return &boolGen{trueProb: trueProb} }

// =============================================================================
// MapGen — probabilistic key-value map.
//
// K(prob, key, ...) accepts either:
//   - a single Gen:               K(0.9, "k8s.pod.uid", UUID())
//   - one or more strings:        K(0.96, "ns", "production", "staging", "development")
//   - a spread slice variable:    K(0.96, "ns", envs...)
//
// The any-typed variadic lets callers pick whichever reads cleanest at the
// call site — the helper resolves it once at profile-build time, never on
// the hot path.
// =============================================================================

type mapEntry struct {
	fixedKey  string
	keyPfx    string
	suffixLen int
	prob      float64
	valueGen  Gen
}

// MapGen produces a map[string]string. Each entry has an independent
// per-row probability of being present.
type MapGen struct {
	entries []mapEntry
	avgKeys int
}

// MapEntryInfo exposes the profile probability and generator for one map attr
type MapEntryInfo struct {
	Key       string
	Prob      float64
	ValueGen  Gen
	SuffixLen int
	RandomKey bool
}

// Entries returns a copy of the map attr definitions for diagnostics
func (g *MapGen) Entries() []MapEntryInfo {
	if g == nil || len(g.entries) == 0 {
		return nil
	}
	out := make([]MapEntryInfo, 0, len(g.entries))
	for _, e := range g.entries {
		key := e.fixedKey
		randomKey := e.keyPfx != ""
		if randomKey {
			key = e.keyPfx
		}
		out = append(out, MapEntryInfo{
			Key:       key,
			Prob:      e.prob,
			ValueGen:  e.valueGen,
			SuffixLen: e.suffixLen,
			RandomKey: randomKey,
		})
	}
	return out
}

// MapKey is a single entry inside a Map(...) builder. Use K or KP to construct.
type MapKey struct{ e mapEntry }

// K declares a fixed-key map entry.
func K(prob float64, key string, args ...any) MapKey {
	return MapKey{e: mapEntry{fixedKey: key, prob: prob, valueGen: argsToGen(key, args)}}
}

// KP declares a random-key map entry: prefix + N-char hex suffix.
// Useful for thrashing compression with many unique keys.
func KP(prob float64, prefix string, suffixLen int, args ...any) MapKey {
	return MapKey{e: mapEntry{keyPfx: prefix, suffixLen: suffixLen, prob: prob, valueGen: argsToGen(prefix, args)}}
}

// argsToGen dispatches the variadic-any arguments into either a Gen or a Pool.
// Calls panic at startup if the args are malformed — that's a profile bug, not a runtime concern.
func argsToGen(label string, args []any) Gen {
	if len(args) == 0 {
		return Const("")
	}
	if len(args) == 1 {
		if g, ok := args[0].(Gen); ok {
			return g
		}
	}
	strs := make([]string, len(args))
	for i, a := range args {
		s, ok := a.(string)
		if !ok {
			panic(fmt.Sprintf("generate: K/KP(%q): arg %d must be a string or a single Gen; got %T", label, i, a))
		}
		strs[i] = s
	}
	return Pool(strs...)
}

// Map builds a MapGen from a list of MapKey entries.
func Map(keys ...MapKey) *MapGen {
	m := &MapGen{entries: make([]mapEntry, len(keys))}
	sum := 0.0
	for i, k := range keys {
		m.entries[i] = k.e
		sum += k.e.prob
	}
	m.avgKeys = int(sum + 1)
	return m
}

// Generate produces a single map[string]string for a row.
func (g *MapGen) Generate(rng *Rng) map[string]string {
	if g == nil || len(g.entries) == 0 {
		return nil
	}
	m := make(map[string]string, g.avgKeys)
	for i := range g.entries {
		e := &g.entries[i]
		if rng.Float64() >= e.prob {
			continue
		}
		key := e.fixedKey
		if e.keyPfx != "" {
			key = e.keyPfx + rng.HexString(e.suffixLen)
		}
		m[key] = e.valueGen.Generate(rng)
	}
	return m
}
