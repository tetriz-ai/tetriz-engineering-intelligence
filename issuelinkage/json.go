// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import "fmt"

// The engine's results carry int enums. Without a text form they serialise as
// their numbers, so a stored result reads "tier": 3 and means nothing to
// anyone reading it, and renumbering a constant silently rewrites history.
// Each enum below marshals as the name String() gives it and reads back from
// that name. The names live only in String(), so the two directions cannot
// drift apart.
//
// Marshalling never fails: a value outside the enum takes the "unknown" name
// String() already gives it, and reads back as the zero value. Unmarshalling
// rejects every other text, so a typo is an error rather than a silent zero.

// ErrUnknownEnum wraps every rejected enum name.
var ErrUnknownEnum = fmt.Errorf("issuelinkage: unknown enum name")

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

// MarshalText renders the side as "pr" or "issue".
func (s Side) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads a side back from its name.
func (s *Side) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("side", text, []Side{SidePR, SideIssue})
	*s = v
	return err
}

// MarshalText renders the signal type as "text" or "native".
func (t SignalType) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// UnmarshalText reads a signal type back from its name.
func (t *SignalType) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("signal type", text, []SignalType{SignalText, SignalNative})
	*t = v
	return err
}

// MarshalText renders the source field as its name, such as "pr_body".
func (f SourceField) MarshalText() ([]byte, error) { return []byte(f.String()), nil }

// UnmarshalText reads a source field back from its name.
func (f *SourceField) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("source field", text, []SourceField{
		FieldPRTitle, FieldPRBody, FieldBranch, FieldIssueTitle, FieldIssueBody, FieldNativeLink,
	})
	*f = v
	return err
}

// MarshalText renders the intent as its name, such as "fixes".
func (i Intent) MarshalText() ([]byte, error) { return []byte(i.String()), nil }

// UnmarshalText reads an intent back from its name.
func (i *Intent) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("intent", text, []Intent{
		IntentBare, IntentFixes, IntentImplements, IntentReferenceOnly, IntentReferencesContext,
	})
	*i = v
	return err
}

// MarshalText renders the section as "none", "positive" or "negative".
func (s Section) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads a section back from its name.
func (s *Section) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("section", text, []Section{SectionNone, SectionPositive, SectionNegative})
	*s = v
	return err
}

// MarshalText renders the bucket as "reference" or "direct".
func (b Bucket) MarshalText() ([]byte, error) { return []byte(b.String()), nil }

// UnmarshalText reads a bucket back from its name.
func (b *Bucket) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("bucket", text, []Bucket{BucketReference, BucketDirect})
	*b = v
	return err
}

// MarshalText renders the tier as "low", "medium" or "high".
func (t Tier) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// UnmarshalText reads a tier back from its name.
func (t *Tier) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("tier", text, []Tier{TierLow, TierMedium, TierHigh})
	*t = v
	return err
}

// MarshalText renders the relation as its name, such as "fixes".
func (r Relation) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalText reads a relation back from its name.
func (r *Relation) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("relation", text, []Relation{
		RelationFixes, RelationImplements, RelationPartOf, RelationReferencesContext, RelationReferenceOnly,
	})
	*r = v
	return err
}

// MarshalText renders the status as its name, such as "partially_processed".
func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText reads a status back from its name.
func (s *Status) UnmarshalText(text []byte) error {
	v, err := unmarshalEnum("status", text, []Status{
		StatusYetToProcess, StatusProcessed, StatusPartiallyProcessed,
		StatusNotFound, StatusToBeReprocessed, StatusMarkedForForceReprocess,
	})
	*s = v
	return err
}
