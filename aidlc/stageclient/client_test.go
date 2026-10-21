// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package stageclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/stageclient"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

type wireArtifact struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Body        string `json:"body"`
}

// fakeService answers /classify-stage as model-service does, from a name -> (bucket, stage) table:
// skills, agents and commands succeed, any other type is skipped, and a name not in the table
// comes back unresolved.
func fakeService(t testing.TB, table map[string][2]string, requests *[][]wireArtifact) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/classify-stage" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Artifacts []wireArtifact `json:"artifacts"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if requests != nil {
			*requests = append(*requests, req.Artifacts)
		}
		results := []map[string]any{}
		for _, a := range req.Artifacts {
			res := map[string]any{"id": a.ID, "outcome": "SKIPPED", "bucket": nil, "stage": nil, "votes": []string{}}
			if strings.HasSuffix(a.Type, "_SKILL") || strings.HasSuffix(a.Type, "_AGENT") || strings.HasSuffix(a.Type, "_COMMAND") {
				if bs, ok := table[a.Name]; ok {
					res = map[string]any{"id": a.ID, "outcome": "SUCCEEDED", "bucket": bs[0], "stage": bs[1],
						"votes": []string{bs[0], bs[0], bs[0]}}
				} else {
					res = map[string]any{"id": a.ID, "outcome": "UNRESOLVED", "bucket": nil, "stage": nil,
						"votes": []string{"code", "plan", "review"}}
				}
			}
			results = append(results, res)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "failed_calls": 0})
	}))
}

// Four artifacts go to model-service: a review skill and a sales-deck agent get verdicts (the deck
// serves no stage), a hook is skipped, and an ambiguous command gets no majority. Only the two
// verdicts enter the stage map; the other two stay unknown.
func Example() {
	srv := fakeService(nil, map[string][2]string{
		"review-pr":  {"review", "AIDLC_STAGE_REVIEW"},
		"sales-deck": {"x_gtm", "AIDLC_STAGE_UNSPECIFIED"},
	}, nil)
	defer srv.Close()

	client := stageclient.NewClient(srv.URL)
	stageMap, err := client.BuildStageMap(context.Background(), []stageclient.Artifact{
		{ID: "a1", Name: "review-pr", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL},
		{ID: "a2", Name: "sales-deck", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_AGENT},
		{ID: "a3", Name: "pre-commit", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK},
		{ID: "a4", Name: "helper", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_COMMAND},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	ids := make([]string, 0, len(stageMap))
	for id := range stageMap {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Println(id, stageMap[id])
	}
	// Output:
	// a1 AIDLC_STAGE_REVIEW
	// a2 AIDLC_STAGE_UNSPECIFIED
}

func TestClassify_SplitsIntoRequestsAndKeepsOrder(t *testing.T) {
	var requests [][]wireArtifact
	table := map[string][2]string{}
	var arts []stageclient.Artifact
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("skill-%d", i)
		table[name] = [2]string{"code", "AIDLC_STAGE_CODE"}
		arts = append(arts, stageclient.Artifact{ID: fmt.Sprint(i), Name: name,
			Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL, Body: "body"})
	}
	srv := fakeService(t, table, &requests)
	defer srv.Close()

	client := stageclient.NewClient(srv.URL)
	client.MaxPerRequest = 2
	got, err := client.Classify(context.Background(), arts)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 3 || len(requests[0]) != 2 || len(requests[2]) != 1 {
		t.Fatalf("requests = %v, want sizes 2, 2, 1", requests)
	}
	if requests[0][0].Type != "HARNESS_ARTIFACT_TYPE_SKILL" || requests[0][0].Body != "body" {
		t.Errorf("first artifact sent as %+v", requests[0][0])
	}
	for i, r := range got.Results {
		if r.ID != fmt.Sprint(i) || r.Outcome != stageclient.Succeeded || r.Stage != aidlcv1.AidlcStage_AIDLC_STAGE_CODE {
			t.Errorf("result %d = %+v", i, r)
		}
	}
}

func TestStageMap_LeavesOutEveryOutcomeButSucceeded(t *testing.T) {
	got := stageclient.StageMap([]stageclient.Result{
		{ID: "s", Outcome: stageclient.Succeeded, Stage: aidlcv1.AidlcStage_AIDLC_STAGE_PLAN},
		{ID: "n", Outcome: stageclient.Succeeded, Stage: aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED},
		{ID: "u", Outcome: stageclient.Unresolved},
		{ID: "d", Outcome: stageclient.Deferred},
		{ID: "k", Outcome: stageclient.Skipped},
	})
	want := map[string]aidlcv1.AidlcStage{
		"s": aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
		"n": aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED,
	}
	if len(got) != len(want) || got["s"] != want["s"] || got["n"] != want["n"] {
		t.Errorf("StageMap = %v, want %v", got, want)
	}
}

func TestClassify_DefaultsToRequestsOf250(t *testing.T) {
	var requests [][]wireArtifact
	var arts []stageclient.Artifact
	for i := 0; i < 501; i++ {
		arts = append(arts, stageclient.Artifact{ID: fmt.Sprint(i), Name: fmt.Sprintf("hook-%d", i),
			Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK})
	}
	srv := fakeService(t, nil, &requests)
	defer srv.Close()

	if _, err := stageclient.NewClient(srv.URL).Classify(context.Background(), arts); err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, r := range requests {
		sizes = append(sizes, len(r))
	}
	if fmt.Sprint(sizes) != "[250 250 1]" {
		t.Errorf("request sizes = %v, want [250 250 1]", sizes)
	}
}

func TestClassify_SendsEachDistinctArtifactOnce(t *testing.T) {
	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
	var requests [][]wireArtifact
	srv := fakeService(t, map[string][2]string{
		"ship-task":   {"deploy", "AIDLC_STAGE_DEPLOY"},
		"review-pr":   {"review", "AIDLC_STAGE_REVIEW"},
		"plan-sprint": {"plan", "AIDLC_STAGE_PLAN"},
	}, &requests)
	defer srv.Close()

	arts := []stageclient.Artifact{
		// Three people's copies of one skill: the same content.
		{ID: "alice-ship", Name: "ship-task", Type: skill, Body: "release it"},
		{ID: "bob-ship", Name: "ship-task", Type: skill, Body: "release it"},
		{ID: "carol-ship", Name: "ship-task", Type: skill, Body: "release it"},
		// The same name with a different body is another artifact, classified on its own.
		{ID: "dave-ship", Name: "ship-task", Type: skill, Body: "plan the release"},
		// The same name as another type is another artifact too.
		{ID: "erin-ship", Name: "ship-task", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_COMMAND, Body: "release it"},
		{ID: "alice-review", Name: "review-pr", Type: skill},
		{ID: "bob-review", Name: "review-pr", Type: skill},
		{ID: "frank-plan", Name: "plan-sprint", Type: skill},
		// The same type, name and body with a different description is another artifact.
		{ID: "gina-ship", Name: "ship-task", Type: skill, Body: "release it", Description: "ships a release"},
	}
	client := stageclient.NewClient(srv.URL)
	client.MaxPerRequest = 2
	got, err := client.Classify(context.Background(), arts)
	if err != nil {
		t.Fatal(err)
	}

	// Six distinct artifacts, sent once each, split into requests of 2 distinct artifacts.
	var sent []string
	for _, r := range requests {
		for _, a := range r {
			sent = append(sent, a.ID)
		}
	}
	if want := "[alice-ship dave-ship erin-ship alice-review frank-plan gina-ship]"; fmt.Sprint(sent) != want {
		t.Errorf("sent %v, want %s", sent, want)
	}
	if len(requests) != 3 {
		t.Errorf("%d requests, want 3", len(requests))
	}

	// One result per input artifact, in input order, each copy carrying its group's verdict.
	if len(got.Results) != len(arts) {
		t.Fatalf("%d results for %d artifacts", len(got.Results), len(arts))
	}
	for i, r := range got.Results {
		if r.ID != arts[i].ID {
			t.Errorf("result %d is for %q, want %q", i, r.ID, arts[i].ID)
		}
	}
	stageMap := stageclient.StageMap(got.Results)
	want := map[string]aidlcv1.AidlcStage{
		"alice-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, "bob-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
		"carol-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, "dave-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
		"erin-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, "alice-review": aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
		"bob-review": aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, "frank-plan": aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
		"gina-ship": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
	}
	if fmt.Sprint(stageMap) != fmt.Sprint(want) {
		t.Errorf("StageMap = %v, want %v", stageMap, want)
	}
	// A copy's votes are its own slice, so changing one result does not change another.
	got.Results[0].Votes[0] = "changed"
	if got.Results[1].Votes[0] == "changed" {
		t.Error("copies share one Votes slice")
	}
}

func TestClassify_GroupsOnTheTextAsSent(t *testing.T) {
	// Two bodies that differ only past the length cap are the same artifact once cut, so the
	// client sends one and gives both ids its result.
	var requests [][]wireArtifact
	srv := fakeService(t, map[string][2]string{"long": {"code", "AIDLC_STAGE_CODE"}}, &requests)
	defer srv.Close()
	base := strings.Repeat("x", stageclient.MaxTextLen)
	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
	got, err := stageclient.NewClient(srv.URL).Classify(context.Background(), []stageclient.Artifact{
		{ID: "a", Name: "long", Type: skill, Body: base + "A"},
		{ID: "b", Name: "long", Type: skill, Body: base + "B"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 1 || len(requests[0]) != 1 {
		t.Fatalf("sent %d requests (first with %d artifacts), want one artifact", len(requests), len(requests[0]))
	}
	a, b := got.Results[0], got.Results[1]
	if a.ID != "a" || b.ID != "b" || a.Stage != b.Stage || a.Bucket != b.Bucket || a.Outcome != b.Outcome {
		t.Errorf("results = %+v, %+v; want ids a, b with one shared result", a, b)
	}
}

func TestClassify_CopiesThatDifferGetTheirOwnVerdict(t *testing.T) {
	// The service answers by body here, so two copies of one name with different bodies can disagree,
	// and the client must keep them apart rather than give one the other's verdict.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Artifacts []wireArtifact `json:"artifacts"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		var results []map[string]any
		for _, a := range req.Artifacts {
			bucket, stage := "plan", "AIDLC_STAGE_PLAN"
			if strings.Contains(a.Body, "release") {
				bucket, stage = "deploy", "AIDLC_STAGE_DEPLOY"
			}
			results = append(results, map[string]any{"id": a.ID, "outcome": "SUCCEEDED", "bucket": bucket, "stage": stage,
				"votes": []string{bucket, bucket, bucket}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results, "failed_calls": 0})
	}))
	defer srv.Close()

	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
	stageMap, err := stageclient.NewClient(srv.URL).BuildStageMap(context.Background(), []stageclient.Artifact{
		{ID: "a", Name: "ship-task", Type: skill, Body: "release it"},
		{ID: "b", Name: "ship-task", Type: skill, Body: "break the work into tasks"},
		{ID: "c", Name: "ship-task", Type: skill, Body: "release it"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]aidlcv1.AidlcStage{
		"a": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
		"b": aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
		"c": aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
	}
	if fmt.Sprint(stageMap) != fmt.Sprint(want) {
		t.Errorf("StageMap = %v, want %v", stageMap, want)
	}
}

func TestClassify_CutsLongTextAtACharacterBoundary(t *testing.T) {
	var requests [][]wireArtifact
	srv := fakeService(t, nil, &requests)
	defer srv.Close()
	// Two-byte characters, so a byte-count cut would split one.
	long := strings.Repeat("é", stageclient.MaxTextLen+7)
	short := "kept as is"
	_, err := stageclient.NewClient(srv.URL).Classify(context.Background(), []stageclient.Artifact{
		{ID: "a", Name: "x", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL, Body: long, Description: short},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := requests[0][0]
	if n := utf8.RuneCountInString(got.Body); n != stageclient.MaxTextLen || !utf8.ValidString(got.Body) {
		t.Errorf("body sent as %d characters (valid UTF-8: %v), want %d", n, utf8.ValidString(got.Body), stageclient.MaxTextLen)
	}
	if got.Description != short {
		t.Errorf("description sent as %q, want it unchanged", got.Description)
	}
}

func TestClassify_ACancelledContextIsAnError(t *testing.T) {
	srv := fakeService(t, nil, nil)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stageclient.NewClient(srv.URL).Classify(ctx, []stageclient.Artifact{
		{ID: "a", Name: "x", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL},
	})
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("err = %v, want a context-cancelled error", err)
	}
}

func TestClassify_UnreachableServiceIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens there now
	_, err := stageclient.NewClient(url).Classify(context.Background(), []stageclient.Artifact{
		{ID: "a", Name: "x", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL},
	})
	if err == nil {
		t.Fatal("Classify against a closed server returned no error")
	}
}

func TestClassify_RejectsWhatItCannotRead(t *testing.T) {
	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
	one := []stageclient.Artifact{{ID: "a", Name: "x", Type: skill}}
	cases := []struct {
		name   string
		status int
		body   string
		arts   []stageclient.Artifact
		want   string
	}{
		{"status", http.StatusServiceUnavailable, `{}`, one, "status 503"},
		{"server error", http.StatusInternalServerError, `{}`, one, "status 500"},
		{"malformed", 200, `not json`, one, "decoding"},
		{"count", 200, `{"results": [], "failed_calls": 0}`, one, "0 results for 1 artifacts"},
		{"id", 200, `{"results": [{"id": "b", "outcome": "SKIPPED"}], "failed_calls": 0}`, one, `is for "b"`},
		{"stage", 200, `{"results": [{"id": "a", "outcome": "SUCCEEDED", "bucket": "code", "stage": "AIDLC_STAGE_CODING"}]}`, one, "unknown stage"},
		{"outcome", 200, `{"results": [{"id": "a", "outcome": "MAYBE"}]}`, one, "unknown outcome"},
		{"duplicate", 200, `{}`, append(append([]stageclient.Artifact{}, one...), one...), "more than once"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := stageclient.NewClient(srv.URL).Classify(context.Background(), tc.arts)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
