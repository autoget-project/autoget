package stage3planner

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autoget-project/autoget/organizer/internal/ai"
	"github.com/autoget-project/autoget/organizer/internal/metadata"
	"github.com/autoget-project/autoget/organizer/internal/model"
	"github.com/autoget-project/autoget/organizer/internal/ptr"
)

// PornSceneDecision is the per-video-file decision emitted by the porn agent
// LLM. The schema is deliberately minimal: title/performers are never echoed
// back by the LLM, they are read from the Go-side candidate whitelist instead.
type PornSceneDecision struct {
	File string `json:"file"`
	Hit  bool   `json:"hit"`
	Slug string `json:"slug"` // must be a slug actually returned by search_porn; empty when miss
}

// PornAgentResponse is the strict structured output schema of the porn agent.
type PornAgentResponse struct {
	Decisions []PornSceneDecision `json:"decisions"`
}

// pornSearchInput is the search_porn tool argument schema (converted to a
// provider-specific JSON schema via ai.Tool.Parameters).
type pornSearchInput struct {
	Query string `json:"query"`
}

// pornAgentPrompt drives the porn archive agent. It deliberately states the
// matching signals and the ambiguity of filename encodings instead of fixing a
// concrete format, because release naming conventions vary (date group order,
// abbreviated performer names, studio aliases). The agent is expected to
// discover the right interpretation iteratively: each search round returns
// tpdb candidates whose exact fields either confirm or falsify the current
// reading, and the model self-corrects until a credible hit or the budget is
// spent. Go code performs zero semantic pattern matching on filenames.
const pornAgentPrompt = `You are a western porn scene archiving planner. For each video file, identify the real scene or movie it belongs to and decide how it must be archived.

You have exactly one tool:
- search_porn: searches western porn scenes and movies on ThePornDB by keyword query. It returns candidate scenes/movies with slug, title, type (scene or movie), date, site, performers and tags.

How tpdb search works (facts you can rely on):
- The q= parameter is a space-tokenized full-field AND search over title, description, site, performer names and the date field.
- Date tokens only match the ISO YYYY-MM-DD long form (e.g. "2026-07-19"), never "26.07.19" or "20260719".
- Site and performer names are matched loosely but must still be recognizable tokens.

Workflow per file:
1. Read the verified anchors in the input payload when present: the top-level is_vr flag, and in the entities: studio (canonical site name), release_date (confirmed YYYY-MM-DD), actors (full canonical performer names), clean_title (official title). These come from a real web search about this release — prefer them over any reading you would derive from the filename.
2. Extract the weak signals from the filename: studio/site, a date, performer name(s), possible scene title words. Expect any of them to be absent, abbreviated, truncated, or written in a nonstandard way. When a verified anchor contradicts a filename reading (e.g. a different date order, an abbreviated performer), trust the anchor.
3. Build a first query from the least ambiguous tokens and call search_porn.
4. Judge the returned candidates against all signals. If one is clearly the scene, decide hit and stop for this file. Otherwise learn from the round: change what was wrong and search again.
5. Repeat until confirmed, clearly hopeless, or the search budget is spent.

Search strategy (apply judgment, these are not mechanical rules):
- Number groups in filenames almost always encode the release date, but the group order is not standardized: the year may sit at the start or the end, a group may be 2 or 4 digits, and a 2-digit group is ambiguous about its century ("19" could be 2019, "26" could be 2026; "26.07.19" could be read 2026-07-19 or 2019-07-26). Do not bet on a single ordering: when a reading is ambiguous, test the plausible YYYY-MM-DD forms in successive queries. The search results themselves reveal the right reading — the interpretation whose date actually appears on a matching scene is the correct one; keep using it and drop the alternatives.
- The studio/site prefix is a genuine signal: include it when you are reasonably confident of it. Beware that tpdb may store it under an alias or a different spacing/case (TushyRaw vs Tushy Raw vs Tushy, GirlsWay vs Girlsway). If a site-bearing query returns nothing useful, retry without the site or with an alias form.
- Performer tokens are hints, not exact strings: filenames may carry a full name, only a first name, initials, or a truncation ("Azul Hermosa", "Azul H.", "A. Hermosa"). Judge a candidate by whether its performer list plausibly contains the same person(s) the filename points at, corroborated by site/date/title. The candidates also teach you canonical full names for follow-up queries.
- When the filename carries recognizable scene title words (what remains after mentally stripping studio, date, performer and noise tokens like XXX, resolution, codec, release group), query with those words, optionally adding performer/site/date.
- Keep queries short (about 2-4 tokens); every token is an AND filter, so extra words suppress recall.
- A round that returns zero or only unrelated candidates is information, not a dead end: it usually means one token is off — most often the date reading, an abbreviated performer name, or a renamed/aliased studio. Change exactly that token (another date form, the canonical name learned from a previous round, with/without the site) rather than re-running the same losing query.

Hit judgment (a candidate must be real, never guessed):
- Weigh every signal a returned candidate carries: site/studio match (aliases ok), its date matching one of the filename's plausible date readings, its performers plausibly covering the filename's person(s), and title words matching.
- The candidate satisfying the most signals wins. One candidate uniquely matching site + date + performer is a confirmed hit: stop searching for that file and emit it.
- Only emit slugs actually returned by search_porn in this conversation. If no returned candidate is credible after a genuine self-correcting attempt, emit hit=false for that file.

Budget: a few search rounds per file (aim to confirm well before ~5); stop for a file as soon as it is confirmed or clearly hopeless. You may also batch: once you learn a canonical name or the right date reading for one file, reuse that knowledge for related files.

Output: exactly one decision per video file.

Input payload:
`

// buildPornAgentPrompt assembles the agent prompt with the request payload:
// video file names, the Stage 2 enriched title anchor, the raw Stage 1
// entities and the VR flag.
func buildPornAgentPrompt(pc *PlannerContext, videos []string) (string, error) {
	payload := struct {
		Files    []string               `json:"files"`
		Title    string                 `json:"title"`
		Entities map[string]interface{} `json:"entities"`
		IsVR     bool                   `json:"is_vr"`
	}{
		Files:    videos,
		Title:    pc.Metadata.Title,
		Entities: pc.Entities,
		IsVR:     pc.Metadata.IsVR,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal porn agent payload: %w", err)
	}
	return pornAgentPrompt + string(data), nil
}

// planWithAgent runs the ThePornDB tool agent over the request videos and
// converts validated decisions into move actions. Any error aborts the agent
// attempt; the caller degrades to the local naming fallback chain.
func (p *PornPlanner) planWithAgent(ctx context.Context, tp ai.ToolProvider, pc *PlannerContext, videos []string) ([]model.PlanAction, error) {
	prompt, err := buildPornAgentPrompt(pc, videos)
	if err != nil {
		return nil, err
	}

	// slug whitelist: snapshots of every candidate actually returned by the
	// search_porn tool during this Plan call. The tool loop inside
	// GenerateStructuredWithTools runs sequentially, so no locking is needed.
	whitelist := make(map[string]metadata.TPDBVideo)

	searchTool := ai.Tool{
		Name:        "search_porn",
		Description: "Searches western porn scenes and movies on ThePornDB by keyword query. Returns candidate scenes/movies with slug, title, type, date, site, performers and tags.",
		Parameters:  pornSearchInput{},
		Handler: func(ctx context.Context, argsJSON string) (string, error) {
			var in pornSearchInput
			if err := json.Unmarshal([]byte(argsJSON), &in); err != nil {
				return "", fmt.Errorf("invalid search_porn args: %w", err)
			}
			candidates, err := p.tpdb.SearchVideos(ctx, in.Query)
			if err != nil {
				return "", fmt.Errorf("search_porn %q failed: %w", in.Query, err)
			}
			for _, c := range candidates {
				whitelist[c.Slug] = c
			}
			data, err := json.Marshal(candidates)
			if err != nil {
				return "", fmt.Errorf("failed to encode search_porn result: %w", err)
			}
			return string(data), nil
		},
	}

	var resp PornAgentResponse
	if err := tp.GenerateStructuredWithTools(ctx, prompt, []ai.Tool{searchTool}, PornAgentResponse{}, &resp); err != nil {
		return nil, err
	}

	// Decision validation (never trust the LLM): unknown files are ignored,
	// unmentioned videos count as miss, fabricated slugs count as miss, and
	// hit data always comes from the whitelisted candidate snapshot.
	decided := make(map[string]metadata.TPDBVideo, len(videos))
	for _, d := range resp.Decisions {
		if !slices.Contains(videos, d.File) {
			continue
		}
		if _, ok := decided[d.File]; ok {
			continue // first hit decision for a file wins; an earlier miss does not block a later hit
		}
		if !d.Hit {
			continue
		}
		cand, ok := whitelist[d.Slug]
		if !ok {
			continue
		}
		decided[d.File] = cand
	}

	root := string(model.TargetDirPorn)
	if pc.Metadata.IsVR || pornDecidedIsVR(decided) {
		root = string(model.TargetDirPornVR)
	}

	// Actor directory layer: scan the CURRENT active root once; a hit is
	// layered under the first performer (in candidate order) whose name
	// exactly matches an existing directory name.
	performerDirs := pornPerformerDirs(p.targetDir, root)

	// Per-slug occurrence counters assign .part.N in input file order.
	slugTotal := make(map[string]int, len(videos))
	for _, v := range videos {
		if cand, ok := decided[v]; ok {
			slugTotal[cand.Slug]++
		}
	}
	slugSeen := make(map[string]int, len(slugTotal))

	actions := make([]model.PlanAction, 0, len(videos))
	for _, v := range videos {
		cand, hit := decided[v]
		var target string
		if hit {
			title := sanitizeSceneTitle(cand.Title)
			if title == "" {
				// a title collapsing to empty would produce "porn/.mp4";
				// fall back to the slug, which is a safe single token.
				title = cand.Slug
			}
			name := title
			if slugTotal[cand.Slug] > 1 {
				slugSeen[cand.Slug]++
				name = fmt.Sprintf("%s.part.%d", title, slugSeen[cand.Slug])
			}
			target = path.Join(root, pornPerformerLayer(cand, performerDirs), title, name+filepath.Ext(v))
		} else {
			// miss / unmentioned / fabricated slug: per-file fallback chain.
			name := pornDisplayName(pc, v)
			target = path.Join(root, name, name+filepath.Ext(v))
		}
		actions = append(actions, model.PlanAction{File: v, Action: "move", Target: ptr.Str(target)})
	}
	return actions, nil
}

// pornDecidedIsVR reports whether any confirmed hit candidate is a tpdb VR
// scene (tagged "Virtual Reality"). tpdb tags are authoritative metadata, so a
// confirmed hit overrides a missing or uncertain Stage 1 VR flag without any
// filename pattern heuristic.
func pornDecidedIsVR(decided map[string]metadata.TPDBVideo) bool {
	for _, c := range decided {
		for _, tag := range c.Tags {
			if strings.EqualFold(tag, "Virtual Reality") {
				return true
			}
		}
	}
	return false
}

// pornPerformerDirs lists the existing directory names directly under
// targetDir/root (empty when targetDir is unset or the directory is missing).
func pornPerformerDirs(targetDir, root string) map[string]struct{} {
	dirs := make(map[string]struct{})
	if targetDir == "" {
		return dirs
	}
	entries, err := os.ReadDir(filepath.Join(targetDir, root))
	if err != nil {
		return dirs
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs[e.Name()] = struct{}{}
		}
	}
	return dirs
}

// pornPerformerLayer returns the first performer of the candidate (in order)
// matching an existing directory name, or "" when none matches. Performer
// names are sanitized for path separators before matching, mirroring title
// sanitization (Stage 4 still guards traversal as the final backstop).
func pornPerformerLayer(cand metadata.TPDBVideo, performerDirs map[string]struct{}) string {
	for _, perf := range cand.Performers {
		safe := sanitizeSceneTitle(perf)
		if safe == "" {
			continue
		}
		if _, ok := performerDirs[safe]; ok {
			return safe
		}
	}
	return ""
}

// sceneTitleSanitizer maps filesystem-illegal characters to spaces in one pass.
var sceneTitleSanitizer = strings.NewReplacer(
	`/`, " ", `\`, " ", `:`, " ", `*`, " ", `?`, " ", `"`, " ", `<`, " ", `>`, " ", `|`, " ",
)

// sanitizeSceneTitle makes a TPDB title safe as a single path segment:
// illegal characters become spaces and whitespace runs collapse.
func sanitizeSceneTitle(s string) string {
	return strings.Join(strings.Fields(sceneTitleSanitizer.Replace(strings.TrimSpace(s))), " ")
}
