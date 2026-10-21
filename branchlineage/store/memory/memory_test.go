// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory_test

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(*testing.T) branchlineage.Store { return memory.New() })
}
