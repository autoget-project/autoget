package telemetry

// Span name constants across all stages and handlers.
const (
	SpanHTTPPlan           = "organizer.http.plan"
	SpanPipelineCreatePlan = "organizer.pipeline.CreatePlan"
	SpanStage1Classify     = "organizer.stage1.classify"
	SpanStage2Enrich       = "organizer.stage2.enrich"
	SpanStage3Plan         = "organizer.stage3.plan"
	SpanStage4PostProcess  = "organizer.stage4.postprocess"
)

// Attribute key constants across all stages.
const (
	AttrOrganizerDir            = "organizer.dir"
	AttrOrganizerFilesCount     = "organizer.files_count"
	AttrStage1Category          = "organizer.stage1.category"
	AttrStage1RuleMatched       = "organizer.stage1.rule_matched"
	AttrStage1ArbiterUsed       = "organizer.stage1.arbiter_used"
	AttrStage1SpecialistsJSON   = "organizer.stage1.specialists_json"
	AttrStage1SearchContextJSON = "organizer.stage1.search_context_json"
	AttrStage2EnrichedTitle     = "organizer.stage2.enriched.title"
	AttrStage3Planner           = "organizer.stage3.planner"
)
