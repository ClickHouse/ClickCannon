package metricgen

import "hash/fnv"

// Deterministic mixing primitives. Every series attribute, resource pick, and
// point value is a pure function of seeds mixed through these, no per-series
// state exists anywhere, which is what lets a run span hundreds of millions of
// series in constant memory.

// splitmix64 is the SplitMix64 finalizer: a cheap, high-quality 64-bit mixer.
func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return x
}

func mix2(a, b uint64) uint64 {
	return splitmix64(splitmix64(a) ^ b)
}

func mix3(a, b, c uint64) uint64 {
	return splitmix64(mix2(a, b) ^ c)
}

// fnvHash hashes a string seed into the 64-bit seed domain.
func fnvHash(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

// frac maps a mixed hash to a uniform float64 in [0, 1).
func frac(h uint64) float64 {
	return float64(h>>11) / float64(1<<53)
}

// hexBytes writes n deterministic lowercase hex chars derived from h into a
// new string (used for pod hashes, instance ids, trace/span ids).
func hexBytes(h uint64, n int) string {
	const hexChars = "0123456789abcdef"
	buf := make([]byte, n)
	for i := range buf {
		if i%16 == 0 && i > 0 {
			h = splitmix64(h)
		}
		buf[i] = hexChars[(h>>((i%16)*4))&0xF]
	}
	return string(buf)
}
