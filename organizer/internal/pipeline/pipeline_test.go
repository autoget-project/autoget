package pipeline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/autoget-project/autoget/organizer/internal/ai/mock"
	"github.com/autoget-project/autoget/organizer/internal/model"
	stage1classifier "github.com/autoget-project/autoget/organizer/internal/pipeline/stage1_classifier"
	stage2enricher "github.com/autoget-project/autoget/organizer/internal/pipeline/stage2_enricher"
	"github.com/autoget-project/autoget/organizer/internal/telemetry"
)

func newTestPipeline(t *testing.T, prov *mock.Provider, dir string) *Pipeline {
	t.Helper()
	return NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), dir, "tp-test-target", nil, nil)
}

func TestCreatePlan_TVSeriesFullFlow(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()

	// Seed files: dirty video name, companion subtitle and garbage.
	subDir := filepath.Join(downloadDir, "show1")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	subtitleContent := "1\n00:00:01,000 --> 00:00:02,000\n你好世界\n"
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "show.chs.srt"), []byte(subtitleContent), 0o644))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic naming","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[{"file":"[HDSky] 我的剧 S01E01.mkv","action":"move",
			"target":"tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[{"file":"show.chs.srt","action":"move",
			"matched_video":"[HDSky] 我的剧 S01E01.mkv","language":"Chinese"}]}`,
	})

	p := newTestPipeline(t, prov, downloadDir)

	resp, err := p.CreatePlan(context.Background(), "show1",
		[]string{"[HDSky] 我的剧 S01E01.mkv", "show.chs.srt", "cover.nfo"},
		map[string]interface{}{"title": "我的剧", "year": 2020.0})
	require.NoError(t, err)
	require.Nil(t, resp.Error, "normal planning must keep error null")

	actionsByFile := map[string]model.PlanAction{}
	for _, a := range resp.Plan {
		actionsByFile[a.File] = a
	}

	video := actionsByFile["[HDSky] 我的剧 S01E01.mkv"]
	require.NotNil(t, video.Target)
	assert.Equal(t, "move", video.Action)
	assert.Equal(t, "tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.mkv", *video.Target)

	sub := actionsByFile["show.chs.srt"]
	require.NotNil(t, sub.Target)
	assert.Equal(t, "move", sub.Action)
	assert.Equal(t, "tv_series/Others/我的剧 (2020)/Season 01/我的剧 (2020) S01E01.简体中文.chi.srt", *sub.Target)

	garbage := actionsByFile["cover.nfo"]
	assert.Equal(t, "skip", garbage.Action)
	assert.Nil(t, garbage.Target, "garbage must be skipped with null target")
}

func TestCreatePlan_SubtitlePairingFailureDegradesGracefully(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "movie1")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "sub.srt"), []byte("hello"), 0o644))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"movie","reason":"single feature","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes movie downloads",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Others/电影 (2000)/电影 (2000).mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Error:         errors.New("subtitle pairing offline"),
	})

	p := newTestPipeline(t, prov, downloadDir)

	resp, err := p.CreatePlan(context.Background(), "movie1",
		[]string{"movie.mkv", "sub.srt"},
		map[string]interface{}{"title": "电影", "year": 2000.0})
	require.NoError(t, err, "subtitle pairing failure must degrade, not fail")
	require.Nil(t, resp.Error, "error must stay null on degradation")

	videoFound, subFound := false, false
	for _, a := range resp.Plan {
		if a.File == "movie.mkv" && a.Action == "move" {
			videoFound = true
		}
		if a.File == "sub.srt" {
			subFound = true
		}
	}
	assert.True(t, videoFound, "video plan must survive subtitle failure")
	assert.False(t, subFound, "subtitle must be left unplanned after pairing failure")
}

func TestCreatePlan_UnknownCategoryEmptyPlan(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"unknown","reason":"random junk","entities":{}}`,
	})

	p := newTestPipeline(t, prov, t.TempDir())

	resp, err := p.CreatePlan(context.Background(), "junk", []string{"junk.bin"}, nil)
	require.NoError(t, err)
	assert.Nil(t, resp.Error)
	assert.Empty(t, resp.Plan, "unknown category must return an empty plan")
}

func TestCreatePlan_Stage3LLMFailureIsFatal(t *testing.T) {
	t.Parallel()

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic naming","entities":{}}`,
	})
	// No tv planner rule -> mock provider returns its default error.

	p := newTestPipeline(t, prov, t.TempDir())

	_, err := p.CreatePlan(context.Background(), "show", []string{"show/ep01.mkv"}, nil)
	assert.Error(t, err, "stage 3 LLM failure must surface as a fatal error")
}

func TestCreatePlan_InMemorySpanValidation(t *testing.T) {
	t.Parallel()

	tp, exp := telemetry.NewTestTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer("test-tracer")

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "show_trace")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic series","entities":{"clean_title":"Test Series"}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[{"file":"ep1.mkv","action":"move",
			"target":"tv_series/Others/Test Series (2020)/Season 01/Test Series (2020) S01E01.mkv"}]}`,
	})

	p := NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, "tp-test-target", nil, tracer)

	resp, err := p.CreatePlan(context.Background(), "show_trace", []string{"ep1.mkv"}, map[string]interface{}{"title": "Test Series"})
	require.NoError(t, err)
	require.Len(t, resp.Plan, 1)

	spans := exp.GetSpans()
	require.NotEmpty(t, spans)

	var rootSpanFound, s1SpanFound, s2SpanFound, s3SpanFound, s4SpanFound bool
	for _, s := range spans {
		switch s.Name {
		case telemetry.SpanPipelineCreatePlan:
			rootSpanFound = true
			for _, a := range s.Attributes {
				if string(a.Key) == telemetry.AttrOrganizerDir {
					assert.Equal(t, "show_trace", a.Value.AsString())
				}
			}
		case telemetry.SpanStage1Classify:
			s1SpanFound = true
			for _, a := range s.Attributes {
				if string(a.Key) == telemetry.AttrStage1Category {
					assert.Equal(t, string(model.CategoryTVSeries), a.Value.AsString())
				}
			}
		case telemetry.SpanStage2Enrich:
			s2SpanFound = true
		case telemetry.SpanStage3Plan:
			s3SpanFound = true
			for _, a := range s.Attributes {
				if string(a.Key) == telemetry.AttrStage3Planner {
					assert.Equal(t, string(model.CategoryTVSeries), a.Value.AsString())
				}
			}
		case telemetry.SpanStage4PostProcess:
			s4SpanFound = true
			for _, a := range s.Attributes {
				if string(a.Key) == telemetry.AttrStage4FinalActionsCount {
					assert.Equal(t, int64(1), a.Value.AsInt64())
				}
			}
		}
	}
	assert.True(t, rootSpanFound, "root pipeline span should be recorded")
	assert.True(t, s1SpanFound, "stage 1 span should be recorded")
	assert.True(t, s2SpanFound, "stage 2 span should be recorded")
	assert.True(t, s3SpanFound, "stage 3 span should be recorded")
	assert.True(t, s4SpanFound, "stage 4 span should be recorded")
	assert.Equal(t, "move", resp.Plan[0].Action)
}

func TestCreatePlan_OpenTelemetry_SpanHierarchy(t *testing.T) {
	t.Parallel()

	tp, exp := telemetry.NewTestTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer("test-tracer")

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "show_otel")
	require.NoError(t, os.MkdirAll(subDir, 0o755))

	prov := mock.NewProvider()
	// Stage 1 specialist checkers for video files (candidates: porn, bango_porn, movie, tv_series, music_video)
	prov.AddRule(mock.Rule{
		PromptPattern: "episodic TV series or drama episodes",
		Response: stage1classifier.CheckerResponse{
			Confidence: stage1classifier.ConfidenceYes,
			Reason:     "ep1.mkv is an episodic TV show",
			Entities: stage1classifier.CheckerEntities{
				CleanTitle: "Test Series",
				Year:       2020,
			},
		},
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "Western/general adult video (porn)",
		Response: stage1classifier.CheckerResponse{
			Confidence: stage1classifier.ConfidenceNo,
			Reason:     "not adult content",
		},
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "Japanese adult video (JAV)",
		Response: stage1classifier.CheckerResponse{
			Confidence: stage1classifier.ConfidenceNo,
			Reason:     "no japanese bango",
		},
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "standalone movie or film",
		Response: stage1classifier.CheckerResponse{
			Confidence: stage1classifier.ConfidenceNo,
			Reason:     "episodic series, not a movie",
		},
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "music video (MV)",
		Response: stage1classifier.CheckerResponse{
			Confidence: stage1classifier.ConfidenceNo,
			Reason:     "not a music video",
		},
	})

	// Stage 3 TV planner rule with per-item reason
	prov.AddRule(mock.Rule{
		PromptPattern: "organizes TV series downloads",
		Response: `{"plan":[{"file":"ep1.mkv","action":"move",
			"target":"tv_series/Others/Test Series (2020)/Season 01/Test Series (2020) S01E01.mkv",
			"reason":"first episode of season 1"}]}`,
	})

	pipe := NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, "tp-test-target", nil, tracer)

	// Pass ep1.mkv and cover.nfo (cover.nfo tests Stage 4 forced skip)
	resp, err := pipe.CreatePlan(context.Background(), "show_otel", []string{"ep1.mkv", "cover.nfo"}, map[string]interface{}{"title": "Test Series"})
	require.NoError(t, err)
	require.Len(t, resp.Plan, 2)

	spans := exp.GetSpans()
	require.Len(t, spans, 5, "expected 1 root span and 4 stage spans")

	// Find the root pipeline span
	var rootSpan *tracetest.SpanStub
	spansByName := make(map[string]tracetest.SpanStub)
	for i := range spans {
		s := spans[i]
		spansByName[s.Name] = s
		if s.Name == telemetry.SpanPipelineCreatePlan {
			rootSpan = &s
		}
	}

	require.NotNil(t, rootSpan, "root pipeline span should exist")

	stageNames := []string{
		telemetry.SpanStage1Classify,
		telemetry.SpanStage2Enrich,
		telemetry.SpanStage3Plan,
		telemetry.SpanStage4PostProcess,
	}

	for _, name := range stageNames {
		s, ok := spansByName[name]
		require.True(t, ok, "span %s should exist", name)
		assert.Equal(t, rootSpan.SpanContext.SpanID(), s.Parent.SpanID(), "child span %s parent should match root span ID", name)
	}

	// Verify Root span attributes
	rootAttrMap := make(map[string]interface{})
	for _, attr := range rootSpan.Attributes {
		rootAttrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "show_otel", rootAttrMap[telemetry.AttrOrganizerDir])
	assert.Equal(t, int64(2), rootAttrMap[telemetry.AttrOrganizerFilesCount])
	assert.Equal(t, []string{"ep1.mkv", "cover.nfo"}, rootAttrMap[telemetry.AttrOrganizerFiles])
	require.Contains(t, rootAttrMap, telemetry.AttrOrganizerMetadataJSON)
	assert.Contains(t, rootAttrMap[telemetry.AttrOrganizerMetadataJSON].(string), "Test Series")

	// Verify Stage 1 attributes
	s1 := spansByName[telemetry.SpanStage1Classify]
	attrMap1 := make(map[string]interface{})
	for _, attr := range s1.Attributes {
		attrMap1[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "tv_series", attrMap1[telemetry.AttrStage1Category])
	assert.Equal(t, false, attrMap1[telemetry.AttrStage1RuleMatched])
	assert.Equal(t, false, attrMap1[telemetry.AttrStage1ArbiterUsed])
	require.Contains(t, attrMap1, telemetry.AttrStage1SpecialistsJSON)
	assert.NotEmpty(t, attrMap1[telemetry.AttrStage1SpecialistsJSON])
	assert.Contains(t, attrMap1[telemetry.AttrStage1SpecialistsJSON].(string), "tv_series")

	// Verify Stage 2 attributes
	s2 := spansByName[telemetry.SpanStage2Enrich]
	attrMap2 := make(map[string]interface{})
	for _, attr := range s2.Attributes {
		attrMap2[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, false, attrMap2[telemetry.AttrStage2Skipped])
	assert.Equal(t, "Test Series", attrMap2[telemetry.AttrStage2EnrichedTitle])

	// Verify Stage 3 attributes
	s3 := spansByName[telemetry.SpanStage3Plan]
	attrMap3 := make(map[string]interface{})
	for _, attr := range s3.Attributes {
		attrMap3[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, "tv_series", attrMap3[telemetry.AttrStage3Planner])
	assert.Equal(t, int64(2), attrMap3[telemetry.AttrStage3RawActionsCount])
	require.Contains(t, attrMap3, telemetry.AttrStage3ActionReasonsJSON)
	assert.NotEmpty(t, attrMap3[telemetry.AttrStage3ActionReasonsJSON])
	assert.Contains(t, attrMap3[telemetry.AttrStage3ActionReasonsJSON].(string), "ep1.mkv")
	assert.Contains(t, attrMap3[telemetry.AttrStage3ActionReasonsJSON].(string), "first episode of season 1")

	// Verify Stage 4 attributes
	s4 := spansByName[telemetry.SpanStage4PostProcess]
	attrMap4 := make(map[string]interface{})
	for _, attr := range s4.Attributes {
		attrMap4[string(attr.Key)] = attr.Value.AsInterface()
	}
	assert.Equal(t, int64(0), attrMap4[telemetry.AttrStage4SubtitlesPairedCount])
	assert.Equal(t, int64(2), attrMap4[telemetry.AttrStage4FinalActionsCount])
	require.Contains(t, attrMap4, telemetry.AttrStage4ForcedSkipsJSON)
	assert.NotEmpty(t, attrMap4[telemetry.AttrStage4ForcedSkipsJSON])
	assert.Contains(t, attrMap4[telemetry.AttrStage4ForcedSkipsJSON].(string), "cover.nfo")
	require.Contains(t, attrMap4, telemetry.AttrStage4FinalPlanJSON)
	assert.NotEmpty(t, attrMap4[telemetry.AttrStage4FinalPlanJSON])
	assert.Contains(t, attrMap4[telemetry.AttrStage4FinalPlanJSON].(string), "ep1.mkv")
	assert.Contains(t, attrMap4[telemetry.AttrStage4FinalPlanJSON].(string), "Test Series (2020) S01E01.mkv")
}

func TestCreatePlan_OpenTelemetry_ErrorRecording(t *testing.T) {
	t.Parallel()

	tp, exp := telemetry.NewTestTracerProvider()
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer("test-tracer")

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"tv_series","reason":"episodic series","entities":{"clean_title":"Test Series"}}`,
	})
	// No tv planner rule -> mock returns error on Stage 3

	pipe := NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), t.TempDir(), "tp-test-target", nil, tracer)

	_, err := pipe.CreatePlan(context.Background(), "show_err", []string{"ep1.mkv"}, nil)
	assert.Error(t, err)

	spans := exp.GetSpans()
	require.NotEmpty(t, spans)

	var rootSpan, s3Span *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == telemetry.SpanPipelineCreatePlan {
			rootSpan = &spans[i]
		}
		if spans[i].Name == telemetry.SpanStage3Plan {
			s3Span = &spans[i]
		}
	}

	require.NotNil(t, rootSpan, "root span should exist")
	require.NotNil(t, s3Span, "stage 3 span should exist")

	assert.Equal(t, codes.Error, rootSpan.Status.Code)
	assert.Equal(t, codes.Error, s3Span.Status.Code)
	assert.NotEmpty(t, s3Span.Events, "stage 3 span should record error event")
}

func TestReplan_ReclassifiesAndRunsStage4(t *testing.T) {
	t.Parallel()

	tp, exp := telemetry.NewTestTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test-replan")

	downloadDir := t.TempDir()
	subDir := filepath.Join(downloadDir, "replan_dir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(subDir, "movie.chs.srt"),
		[]byte("1\n00:00:01,000 --> 00:00:02,000\n你好世界\n"), 0o644))

	prov := mock.NewProvider()
	// Stage 1 re-classification: the stale organizer_category is stripped, so
	// the mock provider's legacy single-pass prompt is used.
	prov.AddRule(mock.Rule{
		PromptPattern: "media categorization assistant",
		Response:      `{"category":"movie","reason":"single feature","entities":{}}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a movie file organization plan",
		Response: `{"plan":[{"file":"movie.mkv","action":"move",
			"target":"movie/Chinese/黑客帝国 (1999)/黑客帝国 (1999).mkv"}]}`,
	})
	prov.AddRule(mock.Rule{
		PromptPattern: "subtitle files to match their corresponding video",
		Response: `{"plan":[{"file":"movie.chs.srt","action":"move",
			"matched_video":"movie.mkv","language":"Chinese"}]}`,
	})

	pipe := NewPipeline(prov, stage2enricher.NewEnricher(nil, nil, nil, nil), downloadDir, "tp-test-target", nil, tracer)

	resp, err := pipe.Replan(context.Background(), "replan_dir",
		[]string{"movie.mkv", "movie.chs.srt"},
		map[string]interface{}{"title": "错误的名字", "organizer_category": []string{"porn"}},
		model.ReplanContext{UserHint: "the title is 黑客帝国 and year 1999"},
	)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 2)

	byFile := map[string]model.PlanAction{}
	for _, a := range resp.Plan {
		byFile[a.File] = a
	}
	video := byFile["movie.mkv"]
	require.NotNil(t, video.Target)
	assert.Equal(t, "movie/Chinese/黑客帝国 (1999)/黑客帝国 (1999).mkv", *video.Target)

	// Stage 4 pairing must run during a replan too.
	sub := byFile["movie.chs.srt"]
	require.NotNil(t, sub.Target, "companion subtitle must be paired")
	assert.Equal(t, "movie/Chinese/黑客帝国 (1999)/黑客帝国 (1999).简体中文.chi.srt", *sub.Target)

	// Stage 2 must be skipped; the replan pipeline span + 3 stage spans emitted.
	spans := exp.GetSpans()
	names := map[string]bool{}
	for _, s := range spans {
		names[s.Name] = true
	}
	assert.True(t, names[telemetry.SpanPipelineReplan], "replan pipeline span must exist")
	assert.True(t, names[telemetry.SpanStage1Classify])
	assert.True(t, names[telemetry.SpanStage2Enrich], "replan must run Stage 2 enrichment like a normal plan")
	assert.True(t, names[telemetry.SpanStage3Plan])
	assert.True(t, names[telemetry.SpanStage4PostProcess])

	// The stale organizer_category must never reach any LLM prompt.
	for _, c := range prov.Calls() {
		assert.NotContains(t, c.Prompt, "organizer_category",
			"the stale upstream classification must not be forwarded")
	}
}

func TestReplan_BangoUsesCanonicalActressDirFromStage2(t *testing.T) {
	t.Parallel()

	downloadDir := t.TempDir()

	// Seed the actress store: the canonical library directory is 悠香 while
	// YUUKA is just an alias. Only Stage 2 can resolve this.
	actorFile := filepath.Join(t.TempDir(), "actor.json")
	require.NoError(t, os.WriteFile(actorFile, []byte(`{"悠香":["YUUKA","悠香"]}`), 0o644))
	store := stage2enricher.NewActorStore(actorFile, "", nil)
	enricher := stage2enricher.NewEnricher(nil, nil, store, nil)

	prov := mock.NewProvider()
	prov.AddRule(mock.Rule{
		PromptPattern: "revises a bango (JAV) file organization plan",
		Response:      `{"plan":[{"file":"NAAC-076.mp4","action":"move","target":"jav/悠香/NAAC-076.mp4"}]}`,
	})

	pipe := NewPipeline(prov, enricher, downloadDir, "tp-test-target", nil, nil)

	resp, err := pipe.Replan(context.Background(), "dl",
		[]string{"NAAC-076.mp4"},
		map[string]interface{}{
			"dmm_id": "n_1541naac076tk",
			"actors": []string{"YUUKA"},
		},
		model.ReplanContext{UserHint: "this is a JAV"},
	)
	require.NoError(t, err)
	require.Nil(t, resp.Error)
	require.Len(t, resp.Plan, 1)
	require.NotNil(t, resp.Plan[0].Target)
	assert.Equal(t, "jav/悠香/NAAC-076.mp4", *resp.Plan[0].Target)

	calls := prov.Calls()
	require.Len(t, calls, 1, "dmm_id short-circuits Stage 1; Stage 2 resolves the actress dir without an LLM call")
	assert.Contains(t, calls[0].Prompt, `"actor_dir":"悠香"`,
		"Stage 2 must resolve the canonical actress directory and hand it to the replan prompt")
}
