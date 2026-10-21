// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs_test

import (
	"encoding/json"
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/adapter/vcs"
)

func ExampleEntityKey() {
	fmt.Println(vcs.EntityKey("mr-1!1#merged"))
	// Output:
	// mr-1!1
}

func ExampleIssues() {
	is := vcs.Issues(issuelinkage.VendorJira, []vcs.Record{{
		ExternalID: "10001#snapshot",
		Payload:    json.RawMessage(`{"key":"WORK-1","fields":{"summary":"Export","description":{"type":"doc","content":[{"type":"text","text":"see acme/web#PR-4"}]}}}`),
	}})
	fmt.Println(is[0].Key, is[0].Identifiers, is[0].Body)
	// Output:
	// 10001 [WORK-1] see acme/web#PR-4
}
