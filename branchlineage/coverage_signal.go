// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import "sort"

// The healthy band for a carrier's own changed lines against the summed lines
// of what it carried. Inside it, the derived contents explain the promotion.
const (
	BandLow  = 0.5
	BandHigh = 2.0
)

// Band classifies one carrier's line reconciliation. The exclusions are checked
// in this order, so each carrier lands in exactly one band.
type Band int

const (
	// BandUnknown is the zero value.
	BandUnknown Band = iota
	// BandNoCarrierLines means the carrier has no diff data.
	BandNoCarrierLines
	// BandNoCarriedLines means at least one carried pull request has no diff data.
	BandNoCarriedLines
	// BandZeroCarried means the carried pull requests' lines sum to zero, or it
	// carried nothing.
	BandZeroCarried
	// BandIn means carrier lines are BandLow to BandHigh times the carried sum.
	BandIn
	// BandOut means outside the band. Above it usually means history the merge path
	// cannot see, such as the content a release branch held when it was cut.
	BandOut
)

func (b Band) String() string {
	switch b {
	case BandNoCarrierLines:
		return "no_carrier_lines"
	case BandNoCarriedLines:
		return "no_carried_lines"
	case BandZeroCarried:
		return "zero_carried"
	case BandIn:
		return "in_band"
	case BandOut:
		return "out_of_band"
	default:
		return "unknown"
	}
}

// CarrierCoverage is one carrier's line reconciliation.
type CarrierCoverage struct {
	Carrier string
	// CarrierLines is nil when the carrier has no diff data.
	CarrierLines *int64
	// CarriedSum sums the carried lines that are known.
	CarriedSum int64
	// AnyCarriedUnknown is set when a carried pull request has no diff data.
	AnyCarriedUnknown bool
	Band              Band
}

// Coverage says how much of a repository's tracked-branch activity the merge
// path explains.
type Coverage struct {
	// HasCarriers separates "no promotions" (a repository merging straight into
	// its tracked branch) from promotions the merge path explains poorly.
	HasCarriers bool
	// CarrierCount is the promotions on a flow hop that land on a terminal,
	// whether or not they carried anything.
	CarrierCount int
	// TrackedMergedTotal is the caller's count of pull requests merged into the
	// tracked branches over the same population.
	TrackedMergedTotal int
	// CarrierShare is CarrierCount over TrackedMergedTotal; 0 when the total is 0.
	CarrierShare float64
	// ExplainedInBand counts BandIn carriers.
	ExplainedInBand int
	// The excluded carriers, by band. They leave the explained share's
	// denominator rather than lowering it.
	ExcludedNoCarrierLines int
	ExcludedNoCarriedLines int
	ExcludedZeroCarried    int
	// ExplainedShare is ExplainedInBand over the carriers not excluded; 0 when
	// every carrier is excluded, which the exclusion counts disclose.
	ExplainedShare float64
	// Carriers are sorted by carrier key.
	Carriers []CarrierCoverage
}

// Unexplained returns the carriers outside the band or excluded from it: the
// coverage warning. A carrier listed here either lacks line data or promoted
// lines its derived contents do not account for.
func (c Coverage) Unexplained() []CarrierCoverage {
	var out []CarrierCoverage
	for _, cc := range c.Carriers {
		if cc.Band != BandIn {
			out = append(out, cc)
		}
	}
	return out
}

// ComputeCoverage reconciles every promotion in prs: each merged, non-fork pull
// request whose (head, base) hop is in flow and whose base is a terminal. edges
// supply what each one carried, so a promotion with no edges, such as a release
// branch created with its content already on it, is still counted and lands in
// BandZeroCarried. Pass one repository's pull requests and edges, restricted to
// the promotions merged in the reporting window, and count trackedMergedTotal
// over that same window. Empty flow or terminals counts no carriers.
func ComputeCoverage(prs []PR, edges []Edge, flow []Hop, terminals []string, trackedMergedTotal int) Coverage {
	cov := Coverage{TrackedMergedTotal: trackedMergedTotal}
	if len(flow) > 0 && len(terminals) > 0 {
		inFlow := make(map[Hop]struct{}, len(flow))
		for _, h := range flow {
			inFlow[h] = struct{}{}
		}
		terminal := make(map[string]struct{}, len(terminals))
		for _, t := range terminals {
			terminal[t] = struct{}{}
		}
		byCarrier := map[string]*CarrierCoverage{}
		var order []string
		for _, p := range prs {
			if !p.Merged() || p.CrossRepository {
				continue
			}
			if _, ok := inFlow[Hop{Source: p.HeadBranch, Target: p.BaseBranch}]; !ok {
				continue
			}
			if _, ok := terminal[p.BaseBranch]; !ok {
				continue
			}
			if _, dup := byCarrier[p.Key]; dup {
				continue
			}
			byCarrier[p.Key] = &CarrierCoverage{Carrier: p.Key, CarrierLines: p.lines()}
			order = append(order, p.Key)
		}
		for _, e := range edges {
			cc := byCarrier[e.Carrier]
			if cc == nil {
				continue
			}
			if e.CarriedLines == nil {
				cc.AnyCarriedUnknown = true
			} else {
				cc.CarriedSum += *e.CarriedLines
			}
		}
		sort.Strings(order)
		for _, k := range order {
			cc := byCarrier[k]
			cc.Band = band(*cc)
			switch cc.Band {
			case BandNoCarrierLines:
				cov.ExcludedNoCarrierLines++
			case BandNoCarriedLines:
				cov.ExcludedNoCarriedLines++
			case BandZeroCarried:
				cov.ExcludedZeroCarried++
			case BandIn:
				cov.ExplainedInBand++
			}
			cov.Carriers = append(cov.Carriers, *cc)
		}
	}
	cov.CarrierCount = len(cov.Carriers)
	cov.HasCarriers = cov.CarrierCount > 0
	if trackedMergedTotal > 0 {
		cov.CarrierShare = float64(cov.CarrierCount) / float64(trackedMergedTotal)
	}
	if eligible := cov.CarrierCount - cov.ExcludedNoCarrierLines - cov.ExcludedNoCarriedLines - cov.ExcludedZeroCarried; eligible > 0 {
		cov.ExplainedShare = float64(cov.ExplainedInBand) / float64(eligible)
	}
	return cov
}

func band(c CarrierCoverage) Band {
	switch {
	case c.CarrierLines == nil:
		return BandNoCarrierLines
	case c.AnyCarriedUnknown:
		return BandNoCarriedLines
	case c.CarriedSum == 0:
		return BandZeroCarried
	}
	ratio := float64(*c.CarrierLines) / float64(c.CarriedSum)
	if ratio >= BandLow && ratio <= BandHigh {
		return BandIn
	}
	return BandOut
}
