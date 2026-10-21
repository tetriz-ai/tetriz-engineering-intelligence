// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

// activeEngineers is the organisation's active engineers in the window: the distinct
// people with at least one AI session, or with a merged pull request whose author
// identity maps to a person. Both halves are person ids, so a person counted in both
// counts once here. That is unlike the adoption cohort in foldMergedChange, which
// stays in author-identity space and accepts counting one person twice across two
// identities; breadth cannot, because it compares this set with the presence fold's
// person-id sets, and a doubled denominator would push a stage from org-wide to team.
// An author identity the mapping does not know is left out.
func activeEngineers(sessionPersonIDs []string, prs []MergedPR, authorPersonIDs map[string]string) peopleSet {
	out := newPeopleSet()
	for _, pid := range sessionPersonIDs {
		if pid != "" {
			out[pid] = true
		}
	}
	for i := range prs {
		id := prs[i].AuthorIdentityID
		if id == nil || *id == "" {
			continue
		}
		if pid := authorPersonIDs[*id]; pid != "" {
			out[pid] = true
		}
	}
	return out
}
