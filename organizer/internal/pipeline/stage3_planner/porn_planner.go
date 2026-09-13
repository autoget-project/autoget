package stage3planner

import (
	"context"
	"log"
	"path"
	"path/filepath"
	"strings"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/metadata"
	"github.com/autoget-project/organizer/internal/model"
	"github.com/autoget-project/organizer/internal/ptr"
)

// PornSource is the ThePornDB search source consumed by the porn agent
// (implemented by metadata.ThePornDBClient; fakes in tests).
type PornSource interface {
	SearchVideos(ctx context.Context, query string) ([]metadata.TPDBVideo, error)
}

// PornPlanner plans non-bango porn downloads. When the provider supports tool
// calling and a tpdb source is wired, it runs the ThePornDB search agent;
// otherwise (or on any agent failure) it falls back to the local naming chain
// (L9): entities id -> entities name -> enriched title -> file stem. Targets
// follow porn|porn_vr/{name}/{name}{ext}; companion subtitles are left to
// Stage 4 pairing and non-media files are skipped.
type PornPlanner struct {
	provider  ai.Provider
	targetDir string
	tpdb      PornSource
}

// NewPornPlanner creates a new PornPlanner. tpdb may be nil, disabling the
// ThePornDB agent wiring (fallback chain only).
func NewPornPlanner(provider ai.Provider, targetDir string, tpdb PornSource) *PornPlanner {
	return &PornPlanner{
		provider:  provider,
		targetDir: targetDir,
		tpdb:      tpdb,
	}
}

// Plan generates the porn move plan. When the provider supports tool calling
// (ai.ToolProvider), a tpdb source is wired and the request contains video
// files, the ThePornDB tool agent runs; any agent failure silently degrades
// to the local naming fallback chain, whose output is unchanged from before
// the agent existed.
func (p *PornPlanner) Plan(ctx context.Context, pc *PlannerContext) ([]model.PlanAction, error) {
	videos, _, others := partitionFiles(pc.Files)

	if tp, ok := p.provider.(ai.ToolProvider); ok && p.tpdb != nil && len(videos) > 0 {
		actions, err := p.planWithAgent(ctx, tp, pc, videos)
		if err == nil {
			return append(actions, skipOthers(others)...), nil
		}
		log.Printf("[degrade] porn tpdb agent failed, falling back to local chain: %v", err)
	}

	root := string(model.TargetDirPorn)
	if pc.Metadata.IsVR {
		root = string(model.TargetDirPornVR)
	}

	actions := make([]model.PlanAction, 0, len(videos)+len(others))
	for _, v := range videos {
		name := pornDisplayName(pc, v)
		actions = append(actions, model.PlanAction{
			File:   v,
			Action: "move",
			Target: ptr.Str(path.Join(root, name, name+filepath.Ext(v))),
		})
	}
	for _, o := range others {
		actions = append(actions, model.PlanAction{File: o, Action: "skip"})
	}
	return actions, nil
}

// pornDisplayName implements the L9 naming fallback chain.
func pornDisplayName(pc *PlannerContext, video string) string {
	if pc.Entities != nil {
		if id, ok := pc.Entities["id"].(string); ok && strings.TrimSpace(id) != "" {
			return strings.TrimSpace(id)
		}
		if name, ok := pc.Entities["name"].(string); ok && strings.TrimSpace(name) != "" {
			return strings.TrimSpace(name)
		}
	}
	if t := strings.TrimSpace(pc.Metadata.Title); t != "" {
		return t
	}
	base := filepath.Base(video)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
