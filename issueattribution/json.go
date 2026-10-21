// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution

import "fmt"

// The measure's inputs and results carry int enums. Without a text form they
// serialise as their numbers, so a stored policy reads "min_tier": 3 and
// means nothing to anyone reading it, and renumbering a constant silently
// rewrites history. Each enum below marshals as the name String() gives it
// and reads back from that name, so the two directions cannot drift apart.
//
// Marshalling never fails: a value outside the enum takes the "unknown" name
// String() already gives it, and reads back as the zero value. Unmarshalling
// rejects every other text, so a typo is an error rather than a silent zero.

// ErrUnknownEnum wraps every rejected enum name.
var ErrUnknownEnum = fmt.Errorf("issueattribution: unknown enum name")

// unmarshalEnum finds the value among valid whose name matches text. The
// "unknown" name, which String() gives any value outside the enum, reads
// back as the zero value.
func unmarshalEnum[T fmt.Stringer](kind string, text []byte, valid []T) (T, error) {
	var zero T
	name := string(text)
	for _, v := range valid {
		if v.String() == name {
			return v, nil
		}
	}
	if name == "unknown" {
		return zero, nil
	}
	return zero, fmt.Errorf("%w: %q is not a %s", ErrUnknownEnum, name, kind)
}

// String returns "both", "issue_to_pr" or "pr_to_issue".
func (d Direction) String() string {
	switch d {
	case DirectionBoth:
		return "both"
	case DirectionIssueToPR:
		return "issue_to_pr"
	case DirectionPRToIssue:
		return "pr_to_issue"
	}
	return "unknown"
}

// MarshalText renders the direction as its name.
func (d Direction) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText reads a direction back from its name.
func (d *Direction) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("direction", text, []Direction{
		DirectionBoth, DirectionIssueToPR, DirectionPRToIssue,
	})
	*d = v
	return err
}

// String returns "blended", "authored" or "reviewed".
func (d Dimension) String() string {
	switch d {
	case DimensionBlended:
		return "blended"
	case DimensionAuthored:
		return "authored"
	case DimensionReviewed:
		return "reviewed"
	}
	return "unknown"
}

// MarshalText renders the dimension as its name.
func (d Dimension) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText reads a dimension back from its name.
func (d *Dimension) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("dimension", text, []Dimension{
		DimensionBlended, DimensionAuthored, DimensionReviewed,
	})
	*d = v
	return err
}

// MarshalText renders the tier as its name.
func (t Tier) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// UnmarshalText reads a tier back from its name. TierUnknown is a tier here,
// not an absent one: it is a link the linkage engine did not grade.
func (t *Tier) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("tier", text, []Tier{TierUnknown, TierLow, TierMedium, TierHigh})
	*t = v
	return err
}
