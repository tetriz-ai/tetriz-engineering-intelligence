// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"fmt"
	"sync"
)

// errGroup runs bounded concurrent work and converts a panic in any goroutine
// into an error on Wait.
//
// Panic recovery is the whole reason this is not golang.org/x/sync/errgroup: a
// panic mid-phase would otherwise take the process down, where this engine's
// contract is that a batch fails loudly and the sessions stay pending. The
// phases also pre-populate their result maps, so a goroutine that dies before
// writing leaves a usable zero value rather than a nil dereference downstream.
//
// It does not cancel anything: a failed or panicking worker is recorded and the
// others run to completion, so callers pass their own context to the work.
type errGroup struct {
	sem chan struct{}
	wg  sync.WaitGroup
	mu  sync.Mutex
	err error
}

func newErrGroup() *errGroup {
	return &errGroup{}
}

// setLimit bounds concurrent goroutines. A limit of zero or less is unbounded.
func (g *errGroup) setLimit(n int) {
	if n > 0 {
		g.sem = make(chan struct{}, n)
	}
}

func (g *errGroup) record(err error) {
	g.mu.Lock()
	if g.err == nil {
		g.err = err
	}
	g.mu.Unlock()
}

func (g *errGroup) do(fn func() error) {
	if g.sem != nil {
		g.sem <- struct{}{}
		defer func() { <-g.sem }()
	}
	defer func() {
		if r := recover(); r != nil {
			g.record(fmt.Errorf("panic in attribution worker: %v", r))
		}
	}()
	if err := fn(); err != nil {
		g.record(err)
	}
}

func (g *errGroup) Go(fn func() error) {
	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.do(fn)
	}()
}

func (g *errGroup) Wait() error {
	g.wg.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}
