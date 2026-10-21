// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

// pathCoverage is the share of merged pull requests carrying a file list, the input
// to the path-coverage precondition. A window with no merged pull request is
// vacuously fully covered; the no-activity precondition withholds it instead.
func pathCoverage(prs []MergedPR) (fraction float64, count int) {
	if len(prs) == 0 {
		return 1.0, 0
	}
	withList := 0
	for i := range prs {
		if prs[i].HasFileList {
			withList++
		}
	}
	return float64(withList) / float64(len(prs)), len(prs)
}
