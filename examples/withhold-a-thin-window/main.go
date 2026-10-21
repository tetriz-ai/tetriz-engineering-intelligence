// Command withhold-a-thin-window checks the preconditions for two windows: one with
// no merged pull request, and one where most pull requests carry no file list. Both
// withhold the profile, each for its own reason.
package main

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/fold"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func main() {
	rs := rules.ForVersion("v1")

	// A quiet window: two seat holders, nothing merged.
	quiet := fold.Fold(fold.Facts{Roster: []string{"person-01", "person-02"}})
	report("quiet window", rs, quiet.Preconditions)

	// A window whose connector could not list the files of 7 of its 10 pull requests.
	var prs []fold.MergedPR
	for n := 0; n < 10; n++ {
		author := fmt.Sprintf("gh-person-%02d", n%3)
		prs = append(prs, fold.MergedPR{
			PRKey: fmt.Sprintf("acme/api#%d", 200+n), AuthorIdentityID: &author,
			HasFileList: n < 3, BaseBranch: "main",
		})
	}
	thin := fold.Fold(fold.Facts{MergedPRs: prs})
	report("thin window", rs, thin.Preconditions)
}

func report(name string, rs *rules.RuleSet, pre aidlc.Preconditions) {
	checks, withheld := aidlc.CheckPreconditions(rs, pre)
	fmt.Println(name)
	for _, c := range checks {
		fmt.Printf("  %-36s passed=%-5v %s\n", c.GetKey(), c.GetPassed(), c.GetDetail())
	}
	if withheld == aidlcv1.PreconditionKey_PRECONDITION_KEY_UNSPECIFIED {
		fmt.Println("  published")
		return
	}
	fmt.Println("  withheld by", withheld)
}
