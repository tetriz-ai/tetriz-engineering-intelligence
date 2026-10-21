// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package localsession_test

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/localsession"
)

func ExampleConvert() {
	line := `{"ide_type": "kiro", "session_id": "s1",
		"conversation": [{"timestamp": "2026-03-02T09:00:00.000Z"}],
		"session_meta": {"duration_minutes": 30, "pr_url": "https://example.com/acme/widgets/pull/7",
			"pr_references": [{"pr_number": 7, "pr_repository": "acme/widgets", "source": "SIGNAL_SOURCE_SESSION_COMMAND"}]}}`
	var row localsession.Row
	if err := json.Unmarshal([]byte(line), &row); err != nil {
		log.Fatal(err)
	}
	s, err := localsession.Convert(row, "my-org", "ada")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(s.GetIdeType(), s.GetSessionDurationSeconds(), s.GetAiAttributionConfidence(), s.GetPrReferences()[0].GetSource())
	// Output: IDE_TYPE_KIRO 1800 high SIGNAL_SOURCE_SESSION_COMMAND
}
