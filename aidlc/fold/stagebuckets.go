// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"regexp"
	"strings"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// The deterministic stage classification of a merged pull request from its file
// paths, base branch and title. A PR is bucketed to a stage when its file list
// matches that stage's rule; the code stage is the exception, decided by work type
// (FEATURE or BUGFIX) rather than a path. An absent file list is unmeasurable, never a
// non-match: the fold counts a PR with no file list separately (the path-coverage
// precondition), so these predicates run only over PRs that carry a list.
//
// One PR may serve several stages (a PR touching docs/rfc/ and workflows/ counts in
// both tech_spec and ci_cd): the buckets are not a partition.

var (
	reProductSpecPath = regexp.MustCompile(`(^|/)prds?/|(^|/)prd[-_.]`)
	reDocExt          = regexp.MustCompile(`\.(mdx?|txt|rst|adoc)$`)

	reTechSpecPath    = regexp.MustCompile(`(^|/)(rfcs?|adrs?|decisions?|specs?|design-docs?|proposals?|hld|lld|architecture)/|(^|/)(rfc|adr|tech-spec|tech-design|design-doc)[-_.]`)
	reTechSpecExclude = regexp.MustCompile(`(^|/)prds?/`)

	reTestPathA          = regexp.MustCompile(`((^|/)tests?/|(^|/)src/test/|__tests__|(^|/)(e2e|cypress|integration-tests?)/|_test\.|\.test\.|\.cy\.|(^|/)test[a-z0-9_]*\.(java|kt|cs|py|rb|go)$)`)
	reTestSpec           = regexp.MustCompile(`(_spec\.|\.spec\.)`)
	reTestSpecExcludeExt = regexp.MustCompile(`\.(mdx?|txt|rst|adoc|ya?ml|json)$`)

	reCIPath = regexp.MustCompile(`((^|/)infra/|\.tf$|(^|/)helm(/|-)|dockerfile|jenkinsfile|(^|/)\.github/workflows|(^|/)\.gitlab-ci|(^|/)\.circleci|(^|/)bitbucket-pipelines|(^|/)\.buildkite|(^|/)azure-pipelines)`)

	reMonitorPath = regexp.MustCompile(`(^|/)(monitoring|observability|runbooks?|grafana|prometheus|alertmanager|sre|slo)/|(^|/)(alert|dashboard)s?/[^/]*\.(ya?ml|json|jsonnet|libsonnet|tf)$|\.rules\.ya?ml$`)

	reReleaseBranch = regexp.MustCompile(`^(release|hotfix)[/-]|^rc[-/]|^v?\d+\.\d+|^(uat|staging|prod|production)$`)
	reReleaseTitle  = regexp.MustCompile(`^(chore\()?(release|deploy|promote)\b|^release[-: ]|\brelease v?\d|cut (a )?release`)

	// reObservabilityTool matches an observability tool's name as a whole token; see
	// IsObservabilityTool.
	reObservabilityTool = regexp.MustCompile(`(^|[^a-z0-9])(` + observabilityToolPattern + `)($|[^a-z0-9])`)
)

// observabilityToolPattern is the observability tool names the operate L1 signal
// counts, matched against a lower-cased tool name as a whole token. elasticsearch is listed beside elastic because a whole-token match does not find
// it inside the longer name.
const observabilityToolPattern = `datadog|grafana|sentry|newrelic|new_relic|opensearch|cloudwatch|splunk|honeycomb|kibana|elastic|elasticsearch|pagerduty|opsgenie|bugsnag|rollbar|dynatrace|appdynamics|lightstep|signoz|loki|tempo|jaeger`

// IsObservabilityTool reports whether a session tool's name is one the operate L1
// signal counts: count a person in ObservabilityRead.SeatHolders when a session of
// theirs used one. The match is case-insensitive and on a whole token, a run of
// letters and digits, so "mcp__datadog__query" matches and "temporal_workflow" does
// not.
func IsObservabilityTool(name string) bool {
	return reObservabilityTool.MatchString(strings.ToLower(name))
}

// lowerPaths returns the paths lower-cased, so the stage rules ignore case.
func lowerPaths(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = strings.ToLower(p)
	}
	return out
}

// anyPathMatches reports whether any of the PR's (already lower-cased) paths matches
// re. Callers pass the exact per-stage predicate; the "product-spec / tech-spec need a
// doc extension too" and "test excludes doc/config spec files" refinements are folded
// into the stage helpers below rather than the caller.
func anyPathMatches(paths []string, re *regexp.Regexp) bool {
	for _, p := range paths {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// touchesProductSpec reports the product-spec (PRD) path rule: a prd/ dir or a
// prd-prefixed file, with a doc extension. Both conditions must hold on the same path.
func touchesProductSpec(paths []string) bool {
	for _, p := range paths {
		if reProductSpecPath.MatchString(p) && reDocExt.MatchString(p) {
			return true
		}
	}
	return false
}

// touchesTechSpec reports the tech-spec (design-doc) path rule: a spec/rfc/adr home
// with a doc extension, excluding PRD paths (which belong to
// product spec).
func touchesTechSpec(paths []string) bool {
	for _, p := range paths {
		if reTechSpecPath.MatchString(p) && reDocExt.MatchString(p) && !reTechSpecExclude.MatchString(p) {
			return true
		}
	}
	return false
}

// touchesTests reports the test-path rule: a test tree or filename, or a spec-form
// filename that is not a doc/config file (so api.spec.md is not a test).
func touchesTests(paths []string) bool {
	for _, p := range lowerPaths(paths) {
		if reTestPathA.MatchString(p) {
			return true
		}
		if reTestSpec.MatchString(p) && !reTestSpecExcludeExt.MatchString(p) {
			return true
		}
	}
	return false
}

// touchesCI reports the CI-config path rule (pipelines across vendors, infra-as-code).
func touchesCI(paths []string) bool { return anyPathMatches(paths, reCIPath) }

// touchesMonitoring reports the monitoring-as-code path rule (operate stage).
func touchesMonitoring(paths []string) bool { return anyPathMatches(paths, reMonitorPath) }

// isRelease reports the deploy (release) rule: a release-shaped base branch OR title,
// whatever its case.
// Unlike the other stages this reads the branch and title, not the file list, so a
// release PR with no file list is still classifiable.
func isRelease(baseBranch, title string) bool {
	return reReleaseBranch.MatchString(strings.ToLower(baseBranch)) || reReleaseTitle.MatchString(strings.ToLower(title))
}

// isDelivery reports whether a work type is the code and test & QA delivery
// population: FEATURE or BUGFIX. An unknown work type is not delivery; it is carried
// as the denominator's declared remainder instead, never counted as a hit.
func isDelivery(wt aidlcv1.PRWorkType) bool {
	return wt == aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE ||
		wt == aidlcv1.PRWorkType_PR_WORK_TYPE_BUGFIX
}

// isWorkTypeKnown reports whether a work type was read at all. An unknown work type
// widens the code seat-linked denominator's population rather than being scored as
// non-delivery.
func isWorkTypeKnown(wt aidlcv1.PRWorkType) bool {
	return wt != aidlcv1.PRWorkType_PR_WORK_TYPE_UNSPECIFIED &&
		wt != aidlcv1.PRWorkType_PR_WORK_TYPE_UNCATEGORISED
}

// practiceArtifactTypes is the set of harness artifact types that count as authored
// practice for the presence levels. Installed configuration (hooks, rules,
// CLAUDE.md and AGENTS.md, prompt templates, plugins, permissions) is ambient: it says
// what the organisation installed, not how a person worked, so it never reaches a
// level. UNSPECIFIED is unknown and excluded.
var practiceArtifactTypes = map[aidlcv1.HarnessArtifactType]bool{
	aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL:   true,
	aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_AGENT:   true,
	aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_COMMAND: true,
}

// isPractice reports whether an artifact type is authored practice (vs ambient
// installed configuration).
func isPractice(t aidlcv1.HarnessArtifactType) bool { return practiceArtifactTypes[t] }

// pathStages is the (path/branch-defined stage -> "does this PR serve it") map for one
// PR, used by the merged-change fold. Code and test & QA are not here: code is
// decided by work type, and test & QA reads the delivery population with a
// test-touch filter on the numerator, not a standalone test-path bucket.
func pathStages(paths []string, baseBranch, title string) map[aidlcv1.AidlcStage]bool {
	paths = lowerPaths(paths)
	return map[aidlcv1.AidlcStage]bool{
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: touchesProductSpec(paths),
		aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC:    touchesTechSpec(paths),
		aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD:        touchesCI(paths),
		aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE:      touchesMonitoring(paths),
		aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY:       isRelease(baseBranch, title),
	}
}
