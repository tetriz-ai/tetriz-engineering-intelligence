// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package wiring

import (
	connv1 "github.com/tetriz-ai/tetriz-connectors/gen/lineage/v1"

	dlv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// The two repositories carry the same messages under their own generated types.
// These conversions are mechanical and field-for-field; a field added on either
// side without a line here is simply not carried, so a new field needs a change
// in this file to reach the other repository.

func toConnPending(p *dlv1.PendingLineageBlame) *connv1.PendingLineageBlame {
	if p == nil {
		return nil
	}
	out := &connv1.PendingLineageBlame{
		BugFixPrId:      p.GetBugFixPrId(),
		PlatformRepoId:  p.GetPlatformRepoId(),
		RepoFullName:    p.GetRepoFullName(),
		FixCommitSha:    p.GetFixCommitSha(),
		LocationAnchors: make([]*connv1.LineageBlameAnchor, 0, len(p.GetLocationAnchors())),
	}
	for _, a := range p.GetLocationAnchors() {
		out.LocationAnchors = append(out.LocationAnchors, &connv1.LineageBlameAnchor{
			Path:        a.GetPath(),
			StartLine:   a.GetStartLine(),
			EndLine:     a.GetEndLine(),
			RemovedText: a.GetRemovedText(),
		})
	}
	return out
}

func fromConnPending(p *connv1.PendingLineageBlame) *dlv1.PendingLineageBlame {
	if p == nil {
		return nil
	}
	out := &dlv1.PendingLineageBlame{
		BugFixPrId:      p.GetBugFixPrId(),
		PlatformRepoId:  p.GetPlatformRepoId(),
		RepoFullName:    p.GetRepoFullName(),
		FixCommitSha:    p.GetFixCommitSha(),
		LocationAnchors: make([]*dlv1.LineageBlameAnchor, 0, len(p.GetLocationAnchors())),
	}
	for _, a := range p.GetLocationAnchors() {
		out.LocationAnchors = append(out.LocationAnchors, &dlv1.LineageBlameAnchor{
			Path:        a.GetPath(),
			StartLine:   a.GetStartLine(),
			EndLine:     a.GetEndLine(),
			RemovedText: a.GetRemovedText(),
		})
	}
	return out
}

func toConnResult(r *dlv1.LineageBlameResult) *connv1.LineageBlameResult {
	if r == nil {
		return nil
	}
	out := &connv1.LineageBlameResult{
		Path:              r.GetPath(),
		StartLine:         r.GetStartLine(),
		CulpritSha:        r.GetCulpritSha(),
		CommittedAt:       r.GetCommittedAt(),
		HopsWalked:        r.GetHopsWalked(),
		EvidenceType:      connv1.DefectLineageEvidenceType(r.GetEvidenceType()),
		CulpritPath:       r.GetCulpritPath(),
		CulpritLine:       r.GetCulpritLine(),
		CandidatePrNumber: r.GetCandidatePrNumber(),
	}
	if a := r.GetAuthor(); a != nil {
		out.Author = &connv1.LineageBlameAuthor{Name: a.GetName(), Email: a.GetEmail()}
	}
	return out
}

func fromConnResult(r *connv1.LineageBlameResult) *dlv1.LineageBlameResult {
	if r == nil {
		return nil
	}
	out := &dlv1.LineageBlameResult{
		Path:              r.GetPath(),
		StartLine:         r.GetStartLine(),
		CulpritSha:        r.GetCulpritSha(),
		CommittedAt:       r.GetCommittedAt(),
		HopsWalked:        r.GetHopsWalked(),
		EvidenceType:      dlv1.DefectLineageEvidenceType(r.GetEvidenceType()),
		CulpritPath:       r.GetCulpritPath(),
		CulpritLine:       r.GetCulpritLine(),
		CandidatePrNumber: r.GetCandidatePrNumber(),
	}
	if a := r.GetAuthor(); a != nil {
		out.Author = &dlv1.LineageBlameAuthor{Name: a.GetName(), Email: a.GetEmail()}
	}
	return out
}
