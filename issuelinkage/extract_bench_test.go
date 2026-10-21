// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"strings"
	"testing"
)

// BenchmarkExtractPR_64KiB times one pull request per body shape: 64 KiB of
// references, of headings and references, and of references on one heading
// line.
func BenchmarkExtractPR_64KiB(b *testing.B) {
	for name, unit := range map[string]string{
		"keywords":     "Fixes PROJ-1 see PROJ-2 ",
		"headings":     "## Background\nPROJ-1 ",
		"heading line": "PROJ-1 ",
	} {
		body := strings.Repeat(unit, 64*1024/len(unit))
		if name == "heading line" {
			body = "## " + body
		}
		pr := PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: body}
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ExtractPR(pr, VendorAny, DefaultSignals())
			}
		})
	}
}

// BenchmarkExtractPR_URLs1MiB times 1 MiB of URLs, each followed by a "#N".
func BenchmarkExtractPR_URLs1MiB(b *testing.B) {
	unit := "https://a.co/x #1 "
	pr := PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: strings.Repeat(unit, 1024*1024/len(unit))}
	for i := 0; i < b.N; i++ {
		ExtractPR(pr, VendorAny, DefaultSignals())
	}
}
