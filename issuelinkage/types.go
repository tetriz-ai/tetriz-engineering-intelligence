// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

// Vendor names the tool a record came from, in lower case, as adapter/vcs
// spells it. As
// a candidate's namespace it names the tool an identifier belongs to, and the
// empty Vendor means any tool.
type Vendor string

// The vendors the engine reads.
const (
	VendorAny       Vendor = ""
	VendorGitHub    Vendor = "github"
	VendorGitLab    Vendor = "gitlab"
	VendorBitbucket Vendor = "bitbucket"
	VendorJira      Vendor = "jira"
	VendorLinear    Vendor = "linear"
)

// Side is which entity a record or a row belongs to.
type Side int

// The two sides of a link.
const (
	SidePR Side = iota + 1
	SideIssue
)

// String returns "pr" or "issue".
func (s Side) String() string {
	switch s {
	case SidePR:
		return "pr"
	case SideIssue:
		return "issue"
	}
	return "unknown"
}

// Other returns the counterpart side.
func (s Side) Other() Side {
	if s == SidePR {
		return SideIssue
	}
	return SidePR
}

// Entity names one pull request or issue by its side and key.
type Entity struct {
	Side Side   `json:"side"`
	Key  string `json:"key"`
}

// SignalType names the generator that produced a candidate.
type SignalType int

// The signal types.
const (
	// SignalText is a reference found in a title, body or branch name.
	SignalText SignalType = iota + 1
	// SignalNative is a link the vendor itself records, such as GitHub's
	// closing issue references or a Linear pull-request attachment.
	SignalNative
)

// String returns "text" or "native".
func (t SignalType) String() string {
	switch t {
	case SignalText:
		return "text"
	case SignalNative:
		return "native"
	}
	return "unknown"
}

// SourceField is where a candidate was found.
type SourceField int

// The source fields.
const (
	FieldPRTitle SourceField = iota + 1
	FieldPRBody
	FieldBranch
	FieldIssueTitle
	FieldIssueBody
	FieldNativeLink
)

// String returns the field's name, such as "pr_body".
func (f SourceField) String() string {
	switch f {
	case FieldPRTitle:
		return "pr_title"
	case FieldPRBody:
		return "pr_body"
	case FieldBranch:
		return "branch"
	case FieldIssueTitle:
		return "issue_title"
	case FieldIssueBody:
		return "issue_body"
	case FieldNativeLink:
		return "native_link"
	}
	return "unknown"
}

// Intent is what the words around a candidate say about it.
type Intent int

// The intents.
const (
	// IntentBare is a mention with no keyword before it.
	IntentBare Intent = iota + 1
	// IntentFixes follows a closing keyword (fix, close, resolve), or is a
	// vendor-native link.
	IntentFixes
	// IntentImplements follows a delivering keyword (implement, deliver,
	// complete, ship).
	IntentImplements
	// IntentReferenceOnly follows a referring keyword (see, ref, refs, cc,
	// related to, toward, part of).
	IntentReferenceOnly
	// IntentReferencesContext sits under a heading such as Background or
	// References.
	IntentReferencesContext
)

// String returns the intent's name, such as "fixes".
func (i Intent) String() string {
	switch i {
	case IntentBare:
		return "bare"
	case IntentFixes:
		return "fixes"
	case IntentImplements:
		return "implements"
	case IntentReferenceOnly:
		return "reference_only"
	case IntentReferencesContext:
		return "references_context"
	}
	return "unknown"
}

// Section classifies the Markdown heading a body candidate sits under.
type Section int

// The section classes.
const (
	SectionNone Section = iota + 1
	// SectionPositive is a heading such as Fixes, Closes or Tickets closed.
	SectionPositive
	// SectionNegative is a heading such as Background, Context or References.
	SectionNegative
)

// String returns "none", "positive" or "negative".
func (s Section) String() string {
	switch s {
	case SectionNone:
		return "none"
	case SectionPositive:
		return "positive"
	case SectionNegative:
		return "negative"
	}
	return "unknown"
}

// Bucket is a link's top-level class. A higher value is stronger.
type Bucket int

// The buckets.
const (
	// BucketReference: mentioned, not delivered.
	BucketReference Bucket = iota + 1
	// BucketDirect: fixes, implements or is part of.
	BucketDirect
)

// String returns "reference" or "direct".
func (b Bucket) String() string {
	switch b {
	case BucketReference:
		return "reference"
	case BucketDirect:
		return "direct"
	}
	return "unknown"
}

// Tier sub-classifies a link within its bucket. A higher value is stronger.
type Tier int

// The tiers.
const (
	TierLow Tier = iota + 1
	TierMedium
	TierHigh
)

// String returns "low", "medium" or "high".
func (t Tier) String() string {
	switch t {
	case TierLow:
		return "low"
	case TierMedium:
		return "medium"
	case TierHigh:
		return "high"
	}
	return "unknown"
}

// Relation names how the pull request relates to the issue.
type Relation int

// The relations.
const (
	RelationFixes Relation = iota + 1
	RelationImplements
	// RelationPartOf is a closure claim that lost the closer election.
	RelationPartOf
	RelationReferencesContext
	RelationReferenceOnly
)

// String returns the relation's name, such as "fixes".
func (r Relation) String() string {
	switch r {
	case RelationFixes:
		return "fixes"
	case RelationImplements:
		return "implements"
	case RelationPartOf:
		return "part_of"
	case RelationReferencesContext:
		return "references_context"
	case RelationReferenceOnly:
		return "reference_only"
	}
	return "unknown"
}

// Status is a row's processing state.
type Status int

// The statuses. YetToProcess, ToBeReprocessed and MarkedForForceReprocess are
// armed: the runner processes them next. Processed, PartiallyProcessed and
// NotFound are terminal.
const (
	StatusYetToProcess Status = iota + 1
	StatusProcessed
	StatusPartiallyProcessed
	StatusNotFound
	StatusToBeReprocessed
	StatusMarkedForForceReprocess
)

// String returns the status's name, such as "partially_processed".
func (s Status) String() string {
	switch s {
	case StatusYetToProcess:
		return "yet_to_process"
	case StatusProcessed:
		return "processed"
	case StatusPartiallyProcessed:
		return "partially_processed"
	case StatusNotFound:
		return "not_found"
	case StatusToBeReprocessed:
		return "to_be_reprocessed"
	case StatusMarkedForForceReprocess:
		return "marked_for_force_reprocess"
	}
	return "unknown"
}

// Terminal reports whether the runner has finished with the row.
func (s Status) Terminal() bool {
	return s == StatusProcessed || s == StatusPartiallyProcessed || s == StatusNotFound
}

// Armed reports whether the runner processes the row on its next run.
func (s Status) Armed() bool {
	return s == StatusYetToProcess || s == StatusToBeReprocessed || s == StatusMarkedForForceReprocess
}

// Blocking reasons a link can carry. A link with a reason is reported, but
// weaker than its bucket and tier alone suggest.
const (
	// BlockedSingleSignalFloor: a high-tier link backed by one source field
	// only.
	BlockedSingleSignalFloor = "single_signal_floor"
	// BlockedNegativeSection: every mention sits under a context heading
	// such as Background, Context or Prior art.
	BlockedNegativeSection = "negative_section"
	// BlockedNonElectedCloser: the pull request claimed to close the issue,
	// and a later-merged pull request won the closer election.
	BlockedNonElectedCloser = "non_elected_closer"
)

// Unresolved reasons.
const (
	// ReasonNotFound: no stored record answers to the identifier, which is
	// usual when its counterpart has not arrived yet.
	ReasonNotFound = "not_found"
	// ReasonAmbiguous: more than one stored record answers to it.
	ReasonAmbiguous = "ambiguous"
	// ReasonUnsupported: the identifier has a shape the engine recognises
	// but cannot resolve from this side, such as an issue key in an issue's
	// own text, or a GitLab "mr-" id key. A run does not retry it.
	ReasonUnsupported = "unsupported"
)
