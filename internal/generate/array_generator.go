package generate

import (
	"fmt"
)

// ArrayGen generates array values
type ArrayGen struct {
	minSize int
	maxSize int
	elemGen Gen
}

// Generate creates an array of generated values
func (g *ArrayGen) Generate(rng *Rng) []string {
	size := g.minSize
	if g.maxSize > g.minSize {
		size = g.minSize + rng.IntN(g.maxSize-g.minSize+1)
	}
	
	result := make([]string, size)
	for i := 0; i < size; i++ {
		result[i] = g.elemGen.Generate(rng)
	}
	return result
}

// BuildArrayGenerator creates an array generator from config
func BuildArrayGenerator(cfg ArrayFieldConfig) (*ArrayGen, error) {
	if cfg.MinSize < 0 {
		cfg.MinSize = 0
	}
	if cfg.MaxSize < cfg.MinSize {
		cfg.MaxSize = cfg.MinSize
	}
	
	elemGen, err := BuildGeneratorFromConfig(cfg.Elements)
	if err != nil {
		return nil, fmt.Errorf("failed to build array element generator: %w", err)
	}
	
	return &ArrayGen{
		minSize: cfg.MinSize,
		maxSize: cfg.MaxSize,
		elemGen: elemGen,
	}, nil
}
