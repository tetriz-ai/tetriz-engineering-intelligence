// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fullResult is a result with every field set, so a marshalling test covers
// the whole shape rather than the fields a short example happens to fill.
func fullResult() Result {
	at := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	return Result{
		Entity: Entity{Side: SidePR, Key: "acme/web#PR-7"},
		Status: StatusProcessed,
		Links: []Link{{
			PR: "acme/web#PR-7", Issue: "PROJ-1", From: SidePR,
			Bucket: BucketDirect, Tier: TierHigh, Relation: RelationFixes, Closes: true,
			Signals: []SignalType{SignalText}, Provenance: []SourceField{FieldPRBody},
			Blocking: []string{BlockedSingleSignalFloor},
			Evidence: []Candidate{{
				Signal: SignalText, Identifier: "PROJ-1", Field: FieldPRBody, Heading: "Fixes",
				Intent: IntentFixes, Namespace: VendorJira, Section: SectionPositive,
				Keyword: "fixes", Offset: 6, Length: 6,
			}},
			FirstSeenAt: at, LastCorroboratedAt: at,
		}},
		Unresolved: []Unresolved{{
			Candidate: Candidate{Identifier: "PROJ-9", Offset: -1}, Reason: ReasonNotFound,
		}},
		Closer:     "acme/web#PR-7",
		TitleFlags: []string{"revert"},
	}
}

// TestResult_MarshalsToItsDocumentedShape pins the serialised contract: every
// name is snake_case and every enum is its own name, not the number its
// constant happens to hold.
func TestResult_MarshalsToItsDocumentedShape(t *testing.T) {
	b, err := json.Marshal(fullResult())
	require.NoError(t, err)

	assert.JSONEq(t, `{
	  "entity": {"side": "pr", "key": "acme/web#PR-7"},
	  "status": "processed",
	  "links": [{
	    "pr": "acme/web#PR-7", "issue": "PROJ-1", "from": "pr",
	    "bucket": "direct", "tier": "high", "relation": "fixes", "closes": true,
	    "signals": ["text"], "provenance": ["pr_body"], "blocking": ["single_signal_floor"],
	    "evidence": [{
	      "signal": "text", "identifier": "PROJ-1", "field": "pr_body", "heading": "Fixes",
	      "intent": "fixes", "namespace": "jira", "section": "positive", "keyword": "fixes",
	      "case_folded": false, "offset": 6, "length": 6
	    }],
	    "first_seen_at": "2026-03-04T05:06:07Z", "last_corroborated_at": "2026-03-04T05:06:07Z"
	  }],
	  "unresolved": [{
	    "candidate": {
	      "signal": "unknown", "identifier": "PROJ-9", "field": "unknown", "heading": "",
	      "intent": "unknown", "namespace": "", "section": "unknown", "keyword": "",
	      "case_folded": false, "offset": -1, "length": 0
	    },
	    "reason": "not_found"
	  }],
	  "closer": "acme/web#PR-7", "title_flags": ["revert"], "native_refs_truncated": false
	}`, string(b))
}

// TestResult_SurvivesARoundTrip: a stored result reads back as itself, so the
// contract is usable in both directions.
func TestResult_SurvivesARoundTrip(t *testing.T) {
	b, err := json.Marshal(fullResult())
	require.NoError(t, err)

	var back Result
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, fullResult(), back)
}

// TestEnums_RejectANameTheyDoNotHave: a typo is an error, never a silent zero
// that would read as a weaker tier or the wrong side.
func TestEnums_RejectANameTheyDoNotHave(t *testing.T) {
	var tier Tier
	err := tier.UnmarshalText([]byte("urgent"))
	require.ErrorIs(t, err, ErrUnknownEnum)
	assert.Contains(t, err.Error(), "urgent")

	var side Side
	require.ErrorIs(t, side.UnmarshalText([]byte("")), ErrUnknownEnum)

	err = json.Unmarshal([]byte(`{"tier":"urgent"}`), &Link{})
	require.ErrorIs(t, err, ErrUnknownEnum)
}

// TestEnums_RoundTripEveryName: every value the engine can produce has a name
// that reads back as that value.
func TestEnums_RoundTripEveryName(t *testing.T) {
	for _, v := range []Side{SidePR, SideIssue} {
		var back Side
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Tier{TierLow, TierMedium, TierHigh} {
		var back Tier
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Status{
		StatusYetToProcess, StatusProcessed, StatusPartiallyProcessed,
		StatusNotFound, StatusToBeReprocessed, StatusMarkedForForceReprocess,
	} {
		var back Status
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Relation{
		RelationFixes, RelationImplements, RelationPartOf,
		RelationReferencesContext, RelationReferenceOnly,
	} {
		var back Relation
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []SourceField{
		FieldPRTitle, FieldPRBody, FieldBranch, FieldIssueTitle, FieldIssueBody, FieldNativeLink,
	} {
		var back SourceField
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Intent{
		IntentBare, IntentFixes, IntentImplements, IntentReferenceOnly, IntentReferencesContext,
	} {
		var back Intent
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Section{SectionNone, SectionPositive, SectionNegative} {
		var back Section
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []Bucket{BucketReference, BucketDirect} {
		var back Bucket
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
	for _, v := range []SignalType{SignalText, SignalNative} {
		var back SignalType
		require.NoError(t, back.UnmarshalText([]byte(v.String())))
		assert.Equal(t, v, back)
	}
}

// TestEnums_UnsetReadsBackUnset: "unknown" is the name an unset or
// out-of-range value takes, and it reads back as the zero value rather than
// failing, so a partly-filled record still round-trips.
func TestEnums_UnsetReadsBackUnset(t *testing.T) {
	b, err := json.Marshal(Candidate{Identifier: "PROJ-1"})
	require.NoError(t, err)
	assert.Contains(t, string(b), `"intent":"unknown"`)

	var back Candidate
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, Candidate{Identifier: "PROJ-1"}, back)
}

// TestRecords_ReadBackFromJSON: the input records carry the contract too, so
// a caller can store a batch and read it again.
func TestRecords_ReadBackFromJSON(t *testing.T) {
	pr := PR{
		Key: "acme/web#PR-7", Vendor: VendorGitHub, Repository: "acme/web", Number: 7,
		Title: "Fix it", Body: "Fixes PROJ-1", HeadBranch: "eng-42-fix",
		MergedAt:        time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 3, 4, 5, 6, 8, 0, time.UTC),
		NativeIssueRefs: []NativeRef{{Key: "acme/web#1", Source: VendorGitHub}},
	}
	b, err := json.Marshal(pr)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"head_branch":"eng-42-fix"`)

	var back PR
	require.NoError(t, json.Unmarshal(b, &back))
	assert.Equal(t, pr, back)
}

// TestResult_RefusesAPayloadWrittenBeforeTheNamesExisted: the sqlite store
// keeps a result as JSON in a column, so a database written by an earlier
// build holds Go field names and numeric enums. It is refused, with the row
// named, rather than read: the decoder rejects a number where it wants a
// name, and that is the outcome to want — tolerating the number would let the
// row load while every multi-word field silently stayed empty, because
// "TitleFlags" does not match "title_flags" either. A loud failure on a
// pre-release database is cheaper than a quietly half-read result.
func TestResult_RefusesAPayloadWrittenBeforeTheNamesExisted(t *testing.T) {
	old := `{"Entity": {"Side": 1, "Key": "acme/web#PR-7"}, "Status": 2}`

	err := json.Unmarshal([]byte(old), &Result{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot unmarshal number")
}
