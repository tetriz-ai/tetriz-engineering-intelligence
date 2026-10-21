// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs_test

import (
	"fmt"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/vcs"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

func ExampleConvert() {
	got := vcs.Convert([]vcs.Record{{
		ExternalID: "acme/widgets#PR-7#merged",
		DataDate:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		Metadata:   map[string]string{"repo": "widgets", "owner": "acme", "files_changed": `["limiter/bucket.go"]`},
	}}, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
	fmt.Println(got[0].GetPrRepository(), got[0].GetPrNumber(), got[0].GetFilesChanged())
	// Output: acme/widgets 7 [limiter/bucket.go]
}
