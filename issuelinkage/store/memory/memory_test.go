// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory_test

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(*testing.T) issuelinkage.Store { return memory.New() })
}
