package telemetry

// Span name constants across all stages and handlers.
const (
	SpanHTTPPlan           = "organizer.http.plan"
	SpanHTTPReplan         = "organizer.http.replan"
	SpanHTTPExecute        = "organizer.http.execute"
	SpanPipelineCreatePlan = "organizer.pipeline.CreatePlan"
	SpanStage1Classify     = "organizer.stage1.classify"
	SpanStage2Enrich       = "organizer.stage2.enrich"
	SpanStage3Plan         = "organizer.stage3.plan"
	SpanStage4PostProcess  = "organizer.stage4.postprocess"
)

// Attribute key constants across all stages.
const (
	AttrOrganizerDir               = "organizer.dir"
	AttrOrganizerFilesCount        = "organizer.files_count"
	AttrOrganizerFiles             = "organizer.files"
	AttrOrganizerMetadataJSON      = "organizer.metadata_json"
	AttrStage1Category             = "organizer.stage1.category"
	AttrStage1RuleMatched          = "organizer.stage1.rule_matched"
	AttrStage1ArbiterUsed          = "organizer.stage1.arbiter_used"
	AttrStage1ArbiterReason        = "organizer.stage1.arbiter_reason"
	AttrStage1SpecialistsJSON      = "organizer.stage1.specialists_json"
	AttrStage1SearchContextJSON    = "organizer.stage1.search_context_json"
	AttrStage2Skipped              = "organizer.stage2.skipped"
	AttrStage2EnrichedTitle        = "organizer.stage2.enriched.title"
	AttrStage2EnrichedYear         = "organizer.stage2.enriched.year"
	AttrStage2EnrichedBango        = "organizer.stage2.enriched.bango"
	AttrStage3Planner              = "organizer.stage3.planner"
	AttrStage3RawActionsCount      = "organizer.stage3.raw_actions_count"
	AttrStage3ActionReasonsJSON    = "organizer.stage3.action_reasons_json"
	AttrStage4SubtitlesPairedCount = "organizer.stage4.subtitles_paired_count"
	AttrStage4FinalActionsCount    = "organizer.stage4.final_actions_count"
	AttrStage4ForcedSkipsJSON      = "organizer.stage4.forced_skips_json"
	AttrStage4FinalPlanJSON        = "organizer.stage4.final_plan_json"
)
