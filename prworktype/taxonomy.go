// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

// The bucket slugs each axis may emit — the endpoint contract shared with the
// model-service centroid bundle (contract/taxonomy.py). The Go side checks the
// server's LLM answers against these so a server built from a drifted or wrong
// bundle cannot smuggle an out-of-taxonomy bucket into a result.
var (
	typeBuckets = map[string]bool{
		"feature": true, "bugfix": true, "refactor": true, "chore-deps": true,
		"test": true, "docs": true, "performance": true,
	}
	domainBuckets = map[string]bool{
		"infra-devops": true, "data-pipeline": true, "observability": true,
		"security-compliance": true, "ui-design": true, "ai-ml": true,
	}
)

// validBucket reports whether bucket is one the axis may legitimately emit. The
// type axis also admits the two non-centroid markers the rule and LLM layers
// can set (Uncategorised, CherryPick); the domain axis is centroid-only.
func validBucket(axis, bucket string) bool {
	switch axis {
	case "type":
		return typeBuckets[bucket] || bucket == Uncategorised || bucket == CherryPick
	case "domain":
		return domainBuckets[bucket]
	default:
		return false
	}
}
