// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package fold turns raw facts about an organisation's window (its roster, merged
// pull requests, session-to-pull-request attributions, review records, AI-harness
// usage, enforced practices, planning sessions, branch roles and declarations) into
// the aidlc.Readings the scorer reads, with the coverage and population bookkeeping
// that keeps a partial read from being scored as a confident zero. Fold reads
// nothing itself: the caller supplies every fact.
package fold

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// HarnessInvocation is one person's use of one AI-harness artifact in the window,
// deduplicated to presence: a person either invoked it or did not.
type HarnessInvocation struct {
	PersonID   string
	ArtifactID string
	Type       aidlcv1.HarnessArtifactType
}

// MergedPR is one merged pull request in the window with the attributes the
// merged-change levels read. Whether AI authored it and how many human turns its
// session took arrive separately (Attribution, Facts.HumanTurns), joined by PRKey.
type MergedPR struct {
	// PRKey is the join key the attribution, human-turn, review and planning facts use.
	// An empty PRKey joins nothing: the pull request still counts in the populations its
	// own fields place it in, and is unread on every signal that needs a join.
	PRKey string
	// AuthorIdentityID is the pull request's author identity, the unit the people
	// floors count distinct over. Nil when the pull request carries no author.
	AuthorIdentityID *string
	// AuthorSeatLinked reports whether the author is on the seat roster; the
	// seat-linked denominators divide by these. The flag is the caller's: Fold does not
	// check it against Roster.
	AuthorSeatLinked bool
	// BotDetectionUnsupported is set when the pull request's source cannot flag a
	// review bot, so a zero bot count there is not evidence of no bots: the pull
	// request is left out of both sides of every bot share.
	BotDetectionUnsupported bool
	// BaseBranch and Title feed the release rule, which reads the branch and the
	// title rather than the file list, so a release with no file list is still
	// classifiable.
	BaseBranch string
	Title      string
	// WorkType is the classified kind of work. UNSPECIFIED or UNCATEGORISED is an
	// unknown that widens the code denominator; FEATURE and BUGFIX are delivery.
	WorkType aidlcv1.PRWorkType
	// Paths are the changed file paths; case is ignored. Empty with HasFileList false
	// when the pull request carries no file list; the path-coverage precondition
	// counts exactly that.
	Paths       []string
	HasFileList bool
	// SubmittedReview reports whether the reviewer list carries a submitted review.
	// It does not tell a person from a bot; PRReviewFacts does.
	SubmittedReview bool
	// ReviewersKnown reports whether the reviewer list was captured at all, so an
	// uncaptured list is not read as an unreviewed merge.
	ReviewersKnown bool
	// ComplexityScore is the change complexity, nil when not computed. Code L4
	// reports it as context, never as a gate.
	ComplexityScore *float64
	// RepositoryID is the repository the branch roles are read by; nil when unknown.
	RepositoryID *string
}

// Attribution is the session-to-pull-request link for the window. Only matches the
// caller trusts belong here.
type Attribution struct {
	// AuthoredPRKeys is the set of pull requests an AI session authored: the
	// AI-authored population every merged-change level reads.
	AuthoredPRKeys map[string]struct{}
	// RoutedPRKeys is the authored subset whose session invoked an artifact the
	// harness selected rather than the person. A declared subset, so the routed
	// levels abstain on it.
	RoutedPRKeys map[string]struct{}
	// SessionPersonIDs is the distinct people with at least one AI session in the
	// window, whatever it attributed: the session half of the active engineers.
	SessionPersonIDs []string
}

// PRReviewFacts is one merged pull request's captured review records.
type PRReviewFacts struct {
	// Captured reports whether any review record was captured for the pull request;
	// the captured share bounds the bot levels.
	Captured bool
	// BotPresent reports whether a review bot left a review.
	BotPresent bool
	// HumanRows and BotRows count the captured reviews by people and by bots.
	HumanRows int
	BotRows   int
	// HumanNonAuthorApproved is whether a person other than the author approved;
	// BotApproved whether a review bot did. A self-approval satisfies neither. The
	// declared review policy's approval share reads these.
	HumanNonAuthorApproved bool
	BotApproved            bool
	// ApprovalsUnread says the review records were captured but their approvals were
	// not, so a missing approval is unread rather than absent. An approval that is
	// recorded (HumanNonAuthorApproved or BotApproved) still counts: it wins over the
	// flag. The zero value (false) means a captured record carries its approvals.
	ApprovalsUnread bool
}

// ObservabilityRead is the operate L1 reading over session tool inventories.
type ObservabilityRead struct {
	// SeatHolders is the people whose sessions used an observability tool (see
	// IsObservabilityTool), and People the distinct people behind that count.
	SeatHolders, People int
	// ToolSessions is the sessions in the window that carried a tool inventory at
	// all: the population a declared command could have run in.
	ToolSessions int
	// Coverage is the share of sessions that carried a tool inventory, in [0, 1]. Zero
	// means no inventory was read, and the level abstains as a gap.
	Coverage float64
}

// Enforcement is one live, admin-pushed practice artifact and whom it targets.
type Enforcement struct {
	Type aidlcv1.HarnessArtifactType
	// PersonID and TeamID are the target; both nil means the whole organisation.
	PersonID *string
	TeamID   *string
	// Expired means the enforcement has lapsed: counted, opens nothing.
	Expired bool
	// InstalledIDs are the artifact ids of the copies installed for people, the ids
	// HarnessInvocation and the stage map use. Empty when nothing is installed yet.
	InstalledIDs []string
	// Unplaceable means no installed copy can be derived for the artifact's type.
	Unplaceable bool
}

// EnforcementRead is the enforcement policy with its targets resolved.
type EnforcementRead struct {
	Enforcements []Enforcement
	// TeamMembers maps a targeted team to its members. A team absent from the map
	// could not be resolved, and the stages it touches read as unreadable.
	TeamMembers map[string][]string
	// UnresolvedUsagePersonIDs are the people with a usage record whose artifact
	// could not be identified: use of something unnamed, carried as the L5
	// numerator's unread mass rather than dropped.
	UnresolvedUsagePersonIDs []string
}

// HitlState is what a planned pull request's planning phases say about the human
// gate.
type HitlState int

const (
	// HitlUnknown means no planning phase carried a gate value: unread mass.
	HitlUnknown HitlState = iota
	// HitlAttended means a planning phase ran with a person approving each step.
	HitlAttended
	// HitlUnattended means a planning phase ran without a per-step human gate.
	HitlUnattended
)

// PlanningRead is the Plan stage's evidence.
type PlanningRead struct {
	// PlannerPersonIDs are the people with at least one planning phase in the window.
	PlannerPersonIDs map[string]struct{}
	// PlannedPRKeys are the pull requests an authoring session with a planning phase
	// produced, with the gate the phases recorded.
	PlannedPRKeys map[string]HitlState
	// PlannedPRKeysUnread says the planned set could not be read, so PlannedPRKeys is
	// not evidence that a pull request was unplanned: plan L3's numerator and plan L4's
	// population are unread. When set it overrides PlannedPRKeys, which is then ignored.
	// The zero value (false) means PlannedPRKeys was read.
	PlannedPRKeysUnread bool
	// IssuesInWindow is the tracker issues created in the window: Plan L3's gate.
	IssuesInWindow int
	// ExcludedNullParent counts planning phases that could not be tied to a person;
	// they widen Plan L2's numerator as unread mass.
	ExcludedNullParent int
}

// BranchRoles answers the role of a repository's branch.
type BranchRoles interface {
	// RoleOf resolves one (repository, branch); the zero BranchRole when unknown.
	RoleOf(repositoryID, branch string) BranchRole
	// IsProduction reports whether the branch is a production target: a shipped
	// branch an admin marked, or every shipped branch where none is marked.
	IsProduction(repositoryID, branch string) bool
	// Known reports whether the organisation carries any shipped role at all.
	Known() bool
}

// BranchRole is a resolved branch role.
type BranchRole struct {
	Shipped    bool
	AdminAdded bool
}

// DeclarationState is what could be learned about the admin declarations.
type DeclarationState int

const (
	// DeclarationsOff means declarations are not honoured for this organisation.
	DeclarationsOff DeclarationState = iota
	// DeclarationsUnreadable means they are honoured but could not be read: the
	// cells a declaration opens abstain as unreadable rather than as undeclared.
	DeclarationsUnreadable
	// DeclarationsRead means they were read; Declarations is authoritative.
	DeclarationsRead
)

// Declarations are the admin's declared facts.
type Declarations struct {
	TestCommands []string
	CiCommands   []string
	// ReviewPolicy maps a work type to the approval it requires; absent means no
	// rule.
	ReviewPolicy map[aidlcv1.PRWorkType]ApprovalKind
}

// ApprovalKind is the approval a declared review policy requires for a work type.
type ApprovalKind int

// The approval kinds. ApprovalNone declares no requirement, so it covers nothing.
const (
	ApprovalUnspecified ApprovalKind = iota
	ApprovalHuman
	ApprovalBot
	ApprovalNone
)
