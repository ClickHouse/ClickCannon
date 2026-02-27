package main

import "context"

type StructPool[T any] struct {
	pool chan T
}

func NewStructPool[T any](poolSize int, newInstance func() (T, error)) (*StructPool[T], error) {
	pool := StructPool[T]{
		pool: make(chan T, poolSize),
	}

	for i := 0; i < poolSize; i++ {
		instance, err := newInstance()
		if err != nil {
			return nil, err
		}

		pool.pool <- instance
	}

	return &pool, nil
}

func (p *StructPool[T]) Acquire() T {
	return <-p.pool
}

func (p *StructPool[T]) AcquireCtx(ctx context.Context) (T, bool) {
	select {
	case v := <-p.pool:
		return v, true
	case <-ctx.Done():
		var zero T
		return zero, false
	}
}

func (p *StructPool[T]) Release(v T) {
	p.pool <- v
}

func (p *StructPool[T]) Destroy() {
	for i := 0; i < cap(p.pool); i++ {
		<-p.pool
	}

	close(p.pool)
}
