// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs_test

import (
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/adapter/vcs"
)

func ExampleConvert() {
	prs := vcs.Convert([]vcs.Record{{
		ExternalID: "acme/shop#PR-7#merged",
		DataDate:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Metadata:   map[string]string{"repo": "shop", "owner": "acme", "head_branch": "develop", "target_branch": "main"},
	}})
	p := prs[0]
	fmt.Println(p.Key, p.Repository, p.Number, p.HeadBranch, "->", p.BaseBranch)
	// Output: acme/shop#PR-7#merged acme/shop 7 develop -> main
}
