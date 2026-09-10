package metricgen

import "math"

// Point value synthesis. Every value is a pure function of
// (valueSeed, series index, sweep index), no state is kept between sweeps,
// yet cumulative series are strictly monotonic within a reset epoch and reset
// deterministically, so PromQL-style rate/increase queries over the stored
// data produce sane, verifiable results.
//
// Monotonic-wobble trick used throughout: v(k) = rate*e + w(k) with w(k) in [0, 0.9*rate), monotone within an epoch
// but not a perfectly compressible straight line.

// valueClass shapes the numeric texture of a series (idle/int/decimal/full float) so stored values compress like
// production data: mostly integers, quantized decimals, and idle series, instead of like random noise.
type valueClass int

const (
	classIdle    valueClass = iota // constant gauges, counters that rarely tick
	classInt                       // integral values
	classDecimal                   // rounded to 1-3 decimal places
	classFull                      // full float64 precision
)

// Class mix, cumulative thresholds over [0,1): 12% idle, 38% int, 35%
// decimal, 15% full.
func classFor(sVal uint64) valueClass {
	r := frac(mix2(sVal, 30))
	switch {
	case r < 0.12:
		return classIdle
	case r < 0.50:
		return classInt
	case r < 0.85:
		return classDecimal
	default:
		return classFull
	}
}

var pow10 = [7]float64{1, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6}

// quantize applies the series' precision class to a value. Rounding is a
// non-decreasing map, so monotone sequences stay monotone.
func quantize(v float64, class valueClass, decimals int) float64 {
	switch class {
	case classInt:
		return math.Floor(v)
	case classDecimal:
		p := pow10[decimals]
		return math.Round(v*p) / p
	default:
		return v
	}
}

// seriesRef identifies one series of one metric and carries its derived seeds.
type seriesRef struct {
	def         *metricDef
	j           int    // series index within the metric [0, cardinality)
	resourceIdx int    // owning resource (stable across sweeps)
	sVal        uint64 // per-series value seed (includes app.seed)
}

// class returns the series' value class; valueIsInt metrics are integral by
// construction, so their non-idle classes collapse to int.
func (s seriesRef) class() valueClass {
	c := classFor(s.sVal)
	if s.def.valueIsInt && (c == classDecimal || c == classFull) {
		return classInt
	}
	return c
}

func (s seriesRef) decimals() int {
	return 1 + int(mix2(s.sVal, 31)%3)
}

func makeSeriesRef(def *metricDef, j int, resources int) seriesRef {
	sIdent := mix2(def.structSeed, uint64(j))
	return seriesRef{
		def:         def,
		j:           j,
		resourceIdx: int(sIdent % uint64(resources)),
		sVal:        mix2(def.valueSeed, uint64(j)),
	}
}

// attrDigits decomposes the series index into the metric's mixed-radix
// attribute space, appending the selected prebuilt KeyValues to dst. Each
// j < cardinality yields a unique attribute vector, which is what makes
// per-metric cardinality exact.
func (s seriesRef) appendAttrs(dst []attrKV) []attrKV {
	rem := s.j
	for d := range s.def.dims {
		size := len(s.def.dims[d].values)
		dst = append(dst, s.def.dims[d].values[rem%size])
		rem /= size
	}
	return dst
}

// epochStart returns the sweep (possibly negative = before the run started)
// at which this series' current cumulative epoch began. An epoch ends on a
// deterministic per-series counter reset or on a resource churn generation
// change, whichever came later.
func (s seriesRef) epochStart(p *plan, k uint64) int64 {
	period := 2000 + mix2(s.sVal, 8)%20000
	phase := mix2(s.sVal, 9) % period
	resetStart := int64(k) - int64((k+phase)%period)
	if genStart := int64(p.generationStart(s.resourceIdx, k)); genStart > resetStart {
		return genStart
	}
	return resetStart
}

func (s seriesRef) gaugeValue(k uint64) float64 {
	base := math.Pow(10, frac(mix2(s.sVal, 1))*6-2) // [0.01, 10^4)
	class := s.class()
	if class == classIdle {
		// Idle gauges sit at a constant reading. Gorilla stores these in ~1
		// bit/point, exactly like a real fleet's flat series.
		return quantize(base, classDecimal, 1)
	}

	// Piecewise level shifts every few hundred sweeps model deploys/regime
	// changes; without them the wave is a single stationary process.
	regimePeriod := 500 + mix2(s.sVal, 41)%2000
	level := 0.6 + 0.8*frac(mix3(s.sVal, k/regimePeriod, 42))

	period := float64(60 + mix2(s.sVal, 2)%3540) // sweeps per cycle
	amp := 0.1 + 0.4*frac(mix2(s.sVal, 3))
	phase := frac(mix2(s.sVal, 4))
	noise := (frac(mix3(s.sVal, k, 5)) - 0.5) * 0.1
	v := base * level * (1 + amp*math.Sin(2*math.Pi*(float64(k)/period+phase)) + noise)
	return quantize(v, class, s.decimals())
}

// perSweepRate is the series' average event rate per sweep, log-uniform.
func (s seriesRef) perSweepRate() float64 {
	return math.Pow(10, frac(mix2(s.sVal, 6))*4-1) // [0.1, 1000)
}

// epochRate is the event rate for one cumulative epoch: each reset (or pod
// restart) picks a fresh rate, so counters change slope across epochs while
// staying monotone within one.
func (s seriesRef) epochRate(epochStart int64) float64 {
	return math.Pow(10, frac(mix3(s.sVal, uint64(epochStart), 6))*4-1)
}

// counterValue is the cumulative monotonic sum value at sweep k.
func (s seriesRef) counterValue(k uint64, epochStart int64) float64 {
	e := float64(int64(k) - epochStart)
	class := s.class()
	if class == classIdle {
		// Idle counters tick rarely (think error counters): long flat runs
		// with occasional integer steps, still monotone.
		return math.Floor(s.epochRate(epochStart) * 0.02 * e)
	}
	r := s.epochRate(epochStart)
	v := r*e + frac(mix3(s.sVal, k, 7))*r*0.9
	return quantize(v, class, s.decimals())
}

// deltaSumValue is the per-interval sum for delta temporality.
func (s seriesRef) deltaSumValue(k uint64, monotonic bool) float64 {
	class := s.class()
	if class == classIdle {
		// Mostly-zero windows with a rare small burst.
		if frac(mix3(s.sVal, k, 10)) < 0.05 {
			return math.Floor(1 + frac(mix3(s.sVal, k, 11))*4)
		}
		return 0
	}
	r := s.perSweepRate()
	var v float64
	if monotonic {
		v = r * (0.5 + frac(mix3(s.sVal, k, 10)))
	} else {
		v = r * (frac(mix3(s.sVal, k, 10)) - 0.5) * 2
	}
	return quantize(v, class, s.decimals())
}

// histShape picks the series' bucket weight vector from the metric's
// precomputed shape LUT.
func (s seriesRef) histShape() *histShape {
	return &s.def.histShapes[mix2(s.sVal, 11)%uint64(len(s.def.histShapes))]
}

// monotoneCount is floor(rate*e + wobble): per-bucket cumulative counts that
// never decrease within an epoch (wobble amplitude scales with rate).
func (s seriesRef) monotoneCount(k uint64, epochStart int64, rate float64, salt uint64) uint64 {
	e := float64(int64(k) - epochStart)
	v := rate*e + frac(mix3(s.sVal, k, salt))*rate*0.9
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// deltaCount is one interval's worth of events for one bucket.
func (s seriesRef) deltaCount(k uint64, rate float64, salt uint64) uint64 {
	return uint64(rate * (0.5 + frac(mix3(s.sVal, k, salt))))
}

// histPoint synthesizes one histogram data point. counts has
// len(def.bounds)+1 entries; sum is consistent with the counts via the
// metric's bucket midpoints.
func (s seriesRef) histPoint(k uint64, epochStart int64, counts []uint64) (count uint64, sum, minV, maxV float64) {
	shape := s.histShape()
	cumulative := s.def.temporality == TemporalityCumulative
	rate := s.perSweepRate()
	if cumulative {
		rate = s.epochRate(epochStart)
	}

	for l := range counts {
		bucketRate := rate * shape.weights[l]
		var n uint64
		if cumulative {
			n = s.monotoneCount(k, epochStart, bucketRate, uint64(l)+16)
		} else {
			n = s.deltaCount(k, bucketRate, uint64(l)+16)
		}
		counts[l] = n
		count += n
		sum += float64(n) * s.def.mids[l]
	}
	minV = s.def.mids[shape.minIdx] * 0.8
	maxV = s.def.mids[shape.maxIdx] * 1.2
	return count, sum, minV, maxV
}

// expHistPoint synthesizes one exponential histogram data point into
// pos/neg (neg may come back empty). Offsets are per-series; bucket values
// come from the metric's precomputed pow table.
func (s seriesRef) expHistPoint(k uint64, epochStart int64, pos, neg []uint64) (posOffset, negOffset int32, zeroCount uint64, zeroThreshold float64, count uint64, sum, minV, maxV float64) {
	shape := s.histShape()
	cumulative := s.def.temporality == TemporalityCumulative
	rate := s.perSweepRate()
	if cumulative {
		rate = s.epochRate(epochStart)
	}

	// Offset within the precomputed pow table: table index = bucket - expPowMin.
	posOffset = s.def.expPowMin + int32(mix2(s.sVal, 13)%40)
	mid := func(bucket int32) float64 {
		i := bucket - s.def.expPowMin
		return (s.def.expPow[i] + s.def.expPow[i+1]) / 2
	}

	bucketCount := func(bucketRate float64, salt uint64) uint64 {
		if cumulative {
			return s.monotoneCount(k, epochStart, bucketRate, salt)
		}
		return s.deltaCount(k, bucketRate, salt)
	}

	for l := range pos {
		n := bucketCount(rate*shape.weights[l%len(shape.weights)], uint64(l)+64)
		pos[l] = n
		count += n
		sum += float64(n) * mid(posOffset+int32(l))
	}

	if len(neg) > 0 {
		negOffset = posOffset
		for l := range neg {
			n := bucketCount(rate*0.1*shape.weights[l%len(shape.weights)], uint64(l)+128)
			neg[l] = n
			count += n
			sum -= float64(n) * mid(negOffset+int32(l))
		}
	}

	if mix2(s.sVal, 15)%4 == 0 {
		zeroCount = bucketCount(rate*0.01, 192)
		count += zeroCount
		if mix2(s.sVal, 17)%2 == 0 {
			zeroThreshold = 1e-9
		}
	}

	minV = mid(posOffset) * 0.8
	maxV = mid(posOffset+int32(len(pos))-1) * 1.2
	if len(neg) > 0 {
		minV = -mid(negOffset+int32(len(neg))-1) * 1.2
	}
	return posOffset, negOffset, zeroCount, zeroThreshold, count, sum, minV, maxV
}

// expBucketLens returns the per-series positive/negative bucket window sizes.
func (s seriesRef) expBucketLens() (posLen, negLen int) {
	posLen = 8 + int(mix2(s.sVal, 18)%25) // [8, 32]
	if s.def.hasNegative && mix2(s.sVal, 19)%3 == 0 {
		negLen = max(2, posLen/2)
	}
	return posLen, negLen
}

// summaryPoint synthesizes cumulative count/sum plus per-quantile values
// (values are ordered in q because the noise multiplier is shared).
func (s seriesRef) summaryPoint(k uint64, epochStart int64, qValues []float64) (count uint64, sum float64) {
	rate := s.epochRate(epochStart)
	count = s.monotoneCount(k, epochStart, rate, 20)
	baseLatency := math.Pow(10, frac(mix2(s.sVal, 21))*3-3) // [1ms, 1s)
	sum = float64(count) * baseLatency

	// Latency readings carry limited precision in real systems; idle summaries
	// report constant quantiles.
	noise := 1 + 0.2*(frac(mix3(s.sVal, k, 22))-0.5)
	if s.class() == classIdle {
		noise = 1
	}
	for i, q := range s.def.quantiles {
		qValues[i] = quantize(baseLatency*(1+4*q*q)*noise, classDecimal, 3+s.decimals())
	}
	return count, sum
}

// exemplarFor rolls the per-point exemplar dice. Returns ok=false to skip.
// The exemplar value is a plausible single observation near v.
func (s seriesRef) exemplarFor(p *plan, k uint64, v float64) (value float64, traceHi, traceLo, spanID uint64, ok bool) {
	if p.exemplarThresh == 0 {
		return 0, 0, 0, 0, false
	}
	h := mix3(s.sVal, k, 0x657865)
	if h >= p.exemplarThresh {
		return 0, 0, 0, 0, false
	}
	value = v * (0.5 + frac(splitmix64(h)))
	traceHi = mix3(s.sVal, k, 0x74726831)
	traceLo = mix3(s.sVal, k, 0x74726832)
	spanID = mix3(s.sVal, k, 0x7370616e)
	return value, traceHi, traceLo, spanID, true
}
