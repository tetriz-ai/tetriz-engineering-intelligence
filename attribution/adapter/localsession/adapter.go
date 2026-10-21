// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package localsession turns exported local coding sessions into the evidence
// the engine reads.
//
// A session extractor reads an agent's local files and exports one JSON object
// per session. Row is the shape agent-desk's local-session-extractor writes with
// `export --raw`. Convert turns one row into SessionEvidence by fixed rules:
//
//   - The editor name becomes its enum name ("kiro" becomes "IDE_TYPE_KIRO").
//   - The project name is the last segment of the project path.
//   - The start is the first prompt timestamp, then the editor's creation time,
//     then created_at.
//   - The length is Kiro's own minutes, or the last file change minus the start.
//   - The files edited are the prompts' relevant files and the tracked paths,
//     with home-directory prefixes stripped, unless files_edited is set.
//   - The AI-authorship confidence is "high" for a pr_url or a positive AI-code
//     share, and "none" otherwise.
//
// A row with no session_id, or one that does not decode, is an error rather
// than a quietly emptier session.
package localsession

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
	exportpb "github.com/tetriz-ai/tetriz-engineering-intelligence/internal/exportpb"
)

// Row is one exported session, in the extractor's JSONL export shape. Columns
// the conversion does not read are ignored.
//
// session_meta, ai_attribution and each prompt's metadata are JSON objects. A
// JSON string in their place is refused.
type Row struct {
	IDEType   string `json:"ide_type"`
	SessionID string `json:"session_id"`
	// Conversation is the session's messages, in order.
	Conversation []Prompt `json:"conversation"`
	ProjectPath  *string  `json:"project_path"`
	RepoName     *string  `json:"repo_name"`
	// LastMtimeMS and ConversationSourceCreatedAtMS are milliseconds since the
	// Unix epoch. They may carry a fraction, which is truncated toward zero.
	LastMtimeMS                   *float64        `json:"last_mtime_ms"`
	ConversationSourceCreatedAtMS *float64        `json:"conversation_source_created_at_ms"`
	CreatedAt                     *string         `json:"created_at"`
	SessionMeta                   json.RawMessage `json:"session_meta"`
	AIAttribution                 json.RawMessage `json:"ai_attribution"`
}

// Prompt is one message of an exported session.
type Prompt struct {
	Timestamp *string         `json:"timestamp"`
	Metadata  json.RawMessage `json:"metadata"`
}

// ideTypeToEnum is the extractor's editor names and their enum names.
var ideTypeToEnum = map[string]string{
	"claude-code": "IDE_TYPE_CLAUDE_CODE",
	"cursor":      "IDE_TYPE_CURSOR",
	"copilot":     "IDE_TYPE_COPILOT",
	"antigravity": "IDE_TYPE_ANTIGRAVITY",
	"codex":       "IDE_TYPE_CODEX",
	"kiro":        "IDE_TYPE_KIRO",
}

// IDEType translates an extractor editor name to the evidence contract's enum
// name, by exact lookup. Any value outside the table, an enum name or a
// different case included, is
// IDE_TYPE_UNSPECIFIED. The engine's per-editor rules key on the enum name, so
// an unspecified editor is never gated.
func IDEType(local string) string {
	if enum, ok := ideTypeToEnum[local]; ok {
		return enum
	}
	return "IDE_TYPE_UNSPECIFIED"
}

// ErrNoSessionID is Convert's error for a row with no session_id. The usual
// cause is the extractor's default export, whose camelCase keys leave every
// field of Row empty.
var ErrNoSessionID = errors.New("row has no session_id: export with `local-session-extractor export --raw`")

// Convert turns one exported row into session evidence.
//
// orgID and personID are the caller's: an extractor reads one machine and knows
// neither.
func Convert(row Row, orgID, personID string) (*attributionv1.SessionEvidence, error) {
	if strings.TrimSpace(row.SessionID) == "" {
		return nil, ErrNoSessionID
	}
	conv, err := parse(row)
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", row.SessionID, err)
	}
	sm := conv.GetSessionMeta()

	out := &attributionv1.SessionEvidence{
		// Kept as exported. The export already masks the OS user name in it.
		SessionId:               conv.GetSessionId(),
		PersonId:                personID,
		OrgId:                   orgID,
		IdeType:                 conv.GetIdeType().String(),
		ProjectName:             conv.GetProjectName(),
		RepoName:                deref(row.RepoName),
		FilesEdited:             filesEdited(conv),
		GitBranch:               sm.GetGitBranch(),
		GitBranches:             gitBranches(sm),
		PrNumber:                sm.GetPrNumber(),
		PrRepository:            sm.GetPrRepository(),
		SessionDurationSeconds:  sessionDuration(conv),
		AiAttributionConfidence: confidence(conv),
	}
	if start := conv.GetConversationStartedAt(); start != nil {
		out.StartedAt = start
		out.StartDate = timestamppb.New(start.AsTime().UTC().Truncate(24 * time.Hour))
	}
	for _, b := range sm.GetBranchSignals() {
		out.BranchSignals = append(out.BranchSignals, proto.CloneOf(b))
	}
	for _, r := range sm.GetPrReferences() {
		out.PrReferences = append(out.PrReferences, proto.CloneOf(r))
	}
	return out, nil
}

// parse builds the session payload from the row and decodes it with protojson,
// ignoring unknown fields. Only the fields attribution reads are built.
func parse(row Row) (*exportpb.Conversation, error) {
	prompts := make([]map[string]any, 0, len(row.Conversation))
	for _, p := range row.Conversation {
		prompt := map[string]any{}
		if p.Timestamp != nil {
			prompt["timestamp"] = *p.Timestamp
		}
		if len(p.Metadata) > 0 {
			prompt["metadata"] = p.Metadata
		}
		prompts = append(prompts, prompt)
	}
	payload := map[string]any{
		"ide_type":     IDEType(row.IDEType),
		"session_id":   row.SessionID,
		"project_name": projectName(row.ProjectPath),
		"prompts":      prompts,
	}
	if started, ok := startedAt(row); ok {
		payload["conversation_started_at"] = started
	}
	// The last update is the row's last file change.
	if row.LastMtimeMS != nil {
		payload["last_updated_at"] = utcMillis(fromMillis(*row.LastMtimeMS))
	}
	if len(row.SessionMeta) > 0 {
		payload["session_meta"] = row.SessionMeta
	}
	if len(row.AIAttribution) > 0 {
		payload["ai_attribution"] = row.AIAttribution
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("building session payload: %w", err)
	}
	var conv exportpb.Conversation
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(raw, &conv); err != nil {
		return nil, fmt.Errorf("decoding session payload: %w", err)
	}
	return &conv, nil
}

// projectName keeps the part of the project path after its last "/".
func projectName(path *string) string {
	if path == nil || *path == "" {
		return ""
	}
	return (*path)[strings.LastIndex(*path, "/")+1:]
}

// startedAt is the first prompt timestamp, then the editor's own creation time,
// then the row's created_at. A row with none of the three has no start. A
// created_at with no zone is read as UTC.
func startedAt(row Row) (string, bool) {
	for _, p := range row.Conversation {
		if p.Timestamp != nil {
			return toRFC3339(*p.Timestamp), true
		}
	}
	if row.ConversationSourceCreatedAtMS != nil {
		return utcMillis(fromMillis(*row.ConversationSourceCreatedAtMS)), true
	}
	if row.CreatedAt != nil {
		return toRFC3339(*row.CreatedAt), true
	}
	return "", false
}

// toRFC3339 reads a date-time with "T" or a space between date and time, and
// reformats it. Anything else is passed on unchanged, and decoding refuses it.
func toRFC3339(ts string) string {
	s := ts
	if !strings.Contains(s, "T") {
		s = strings.Replace(s, " ", "T", 1)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return utcMillis(t)
		}
	}
	return ts
}

// utcMillis formats t as UTC RFC 3339 to the millisecond.
func utcMillis(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

// sessionDuration is Kiro's own length in minutes, when positive, in seconds.
// Otherwise it is last update minus start, and zero when that is not positive.
func sessionDuration(conv *exportpb.Conversation) int64 {
	if conv.GetIdeType() == exportpb.IdeType_IDE_TYPE_KIRO {
		if mins := conv.GetSessionMeta().GetDurationMinutes(); mins > 0 {
			return int64(mins * 60)
		}
	}
	start := conv.GetConversationStartedAt()
	end := conv.GetLastUpdatedAt()
	if start == nil || !start.IsValid() || end == nil || !end.IsValid() {
		return 0
	}
	d := end.AsTime().Sub(start.AsTime())
	if d <= 0 {
		return 0
	}
	return int64(d.Seconds())
}

// filesEdited is each prompt's relevant files and the session's tracked
// paths, prefix-stripped and deduplicated. A non-empty session_meta.files_edited
// replaces that set as given. The result is sorted.
func filesEdited(conv *exportpb.Conversation) []string {
	if edited := conv.GetSessionMeta().GetFilesEdited(); len(edited) > 0 {
		files := append([]string(nil), edited...)
		sort.Strings(files)
		return files
	}
	set := map[string]struct{}{}
	add := func(f string) {
		if stripped := stripPathPrefix(f); stripped != "" {
			set[stripped] = struct{}{}
		}
	}
	for _, p := range conv.GetPrompts() {
		for _, f := range p.GetMetadata().GetRelevantFiles() {
			add(f)
		}
	}
	for _, f := range conv.GetSessionMeta().GetTrackedFilePaths() {
		add(f)
	}
	if len(set) == 0 {
		return nil
	}
	files := make([]string, 0, len(set))
	for f := range set {
		files = append(files, f)
	}
	sort.Strings(files)
	return files
}

// stripPathPrefix makes a home-directory path project-relative: it loses its
// user, workspace and project segments, and one too short to have them is
// dropped rather than leak the home path.
func stripPathPrefix(filePath string) string {
	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return ""
	}
	rules := []struct {
		prefix string
		skip   int
	}{
		{"/Users/", 3},
		{"/home/", 3},
		{"/root/", 2},
		{"C:\\Users\\", 3},
		{"C:/Users/", 3},
	}
	for _, r := range rules {
		if strings.HasPrefix(filePath, r.prefix) {
			rest := filePath[len(r.prefix):]
			slashCount := 0
			for i, c := range rest {
				if c == '/' || c == '\\' {
					slashCount++
					if slashCount == r.skip {
						return rest[i+1:]
					}
				}
			}
			return ""
		}
	}
	return filePath
}

// gitBranches fills the list from the single branch when only that is set.
func gitBranches(sm *exportpb.SessionMeta) []string {
	if len(sm.GetGitBranches()) == 0 && sm.GetGitBranch() != "" {
		return []string{sm.GetGitBranch()}
	}
	return sm.GetGitBranches()
}

// confidence is "high" when
// the session names a pull request in pr_url or its AI-code percent (the v2
// measure when above zero, else v1) is above zero, otherwise "none".
// pr_references does not count.
func confidence(conv *exportpb.Conversation) string {
	aa := conv.GetAiAttribution()
	effective := aa.GetAiCodePercent()
	if aa.GetAiCodePercentV2() > 0 {
		effective = aa.GetAiCodePercentV2()
	}
	if conv.GetSessionMeta().GetPrUrl() != "" || effective > 0 {
		return "high"
	}
	return "none"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// fromMillis reads milliseconds since the Unix epoch, truncating any fraction
// toward zero.
func fromMillis(ms float64) time.Time {
	return time.UnixMilli(int64(ms))
}
