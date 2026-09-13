package stage1classifier

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai/mock"
	"github.com/autoget-project/organizer/internal/model"
)

func TestClassifierLLM_AudioDisambiguation(t *testing.T) {
	t.Parallel()

	mockProv := mock.NewProvider()
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Audiobook - Chapter 1.mp3",
		Response: ClassifierLLMResponse{
			Category: model.CategoryAudioBook,
			Reason:   "Filenames represent chapters of an audiobook",
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "01. Taylor Swift - Blank Space.flac",
		Response: ClassifierLLMResponse{
			Category: model.CategoryMusic,
			Reason:   "Track with music artist and song title",
		},
	})

	llm := NewClassifierLLM(mockProv)

	tests := []struct {
		name         string
		files        []string
		wantCategory model.Category
	}{
		{"audio book chapters", []string{"Audiobook - Chapter 1.mp3", "Audiobook - Chapter 2.mp3"}, model.CategoryAudioBook},
		{"music track", []string{"01. Taylor Swift - Blank Space.flac"}, model.CategoryMusic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, err := llm.Classify(context.Background(), tt.files, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCategory, res.Category)
		})
	}
}

func TestClassifyPipeline_Integration(t *testing.T) {
	t.Parallel()

	mockProv := mock.NewProvider()
	mockProv.SetDefaultResponse(ClassifierLLMResponse{
		Category: model.CategoryMovie,
		Reason:   "Extracted movie from noisy release group string",
	}, nil)
	ctx := context.Background()

	// Matched by rule (pure book): the LLM must never be called.
	res1, err := ClassifyPipeline(ctx, mockProv, []string{"document.pdf"}, nil)
	require.NoError(t, err)
	assert.False(t, res1.NeedLLM)
	assert.Equal(t, model.CategoryBook, res1.Category)
	assert.Empty(t, mockProv.Calls(), "rule match must not invoke the LLM")

	// Unmatched by rule (dirty filename): the LLM must be invoked.
	res2, err := ClassifyPipeline(ctx, mockProv, []string{"[HDSky] Inception.2010.1080p.mkv"}, nil)
	require.NoError(t, err)
	assert.True(t, res2.NeedLLM)
	assert.Equal(t, model.CategoryMovie, res2.Category)
	assert.NotEmpty(t, mockProv.Calls(), "LLM must be called for dirty filename")
}

func TestClassifierLLM_GirlsWayPornVsJAV(t *testing.T) {
	t.Parallel()

	mockProv := mock.NewProvider()
	// Porn specialist returns "yes"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Western/general adult video (porn)",
		Response: CheckerResponse{
			Confidence: ConfidenceYes,
			Reason:     "Western adult release with performer names Khloe Kapri and Megan Mistakes",
			Entities: CheckerEntities{
				CleanTitle: "Megan Mistakes Runaway Brides Regret",
				Year:       2026,
				Actors:     []string{"Khloe Kapri", "Megan Mistakes"},
			},
		},
	})
	// JAV bango specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Japanese adult video (JAV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "No Japanese bango code found",
		},
	})
	// Movie specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "standalone movie or film",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Adult video release, not mainstream movie",
		},
	})
	// TV specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "episodic TV series",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not an episodic TV series",
		},
	})
	// Music video specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "music video (MV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not a music video",
		},
	})

	llm := NewClassifierLLM(mockProv)
	files := []string{"GirlsWay Khloe Kapri Megan Mistakes Runaway Brides Regret 2026 2160p WEB-DL H264 AAC2.0-VSEX.mp4"}

	res, err := llm.Classify(context.Background(), files, nil)
	require.NoError(t, err)
	assert.Equal(t, model.CategoryPorn, res.Category, "GirlsWay release must be classified as porn, not bango_porn")
	assert.Equal(t, "Megan Mistakes Runaway Brides Regret", res.Entities["clean_title"])
	assert.Equal(t, 2026, res.Entities["year"])
}

func TestClassifierLLM_ArbiterResolvesConflict(t *testing.T) {
	t.Parallel()

	mockProv := mock.NewProvider()
	// Both porn and movie return "maybe"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Western/general adult video (porn)",
		Response: CheckerResponse{
			Confidence: ConfidenceMaybe,
			Reason:     "Might contain adult themes",
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "standalone movie or film",
		Response: CheckerResponse{
			Confidence: ConfidenceMaybe,
			Reason:     "Looks like a feature film",
		},
	})
	// TV specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "episodic TV series",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not episodic",
		},
	})
	// JAV bango specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Japanese adult video (JAV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "No JAV bango",
		},
	})
	// Music video specialist returns "no"
	mockProv.AddRule(mock.Rule{
		PromptPattern: "music video (MV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not a music video",
		},
	})
	// Arbiter rules in favor of movie
	mockProv.AddRule(mock.Rule{
		PromptPattern: "categorization arbiter",
		Response: ArbiterDecision{
			Category: model.CategoryMovie,
			Reason:   "Arbiter decided it is an indie art movie rather than adult video",
			Entities: CheckerEntities{
				CleanTitle: "Art Movie",
				Year:       2022,
			},
		},
	})

	llm := NewClassifierLLM(mockProv)
	files := []string{"Art Movie 2022.mkv"}

	res, err := llm.Classify(context.Background(), files, nil)
	require.NoError(t, err)
	assert.Equal(t, model.CategoryMovie, res.Category)
	assert.Equal(t, "Art Movie", res.Entities["clean_title"])
}

func TestClassifierLLM_WithSearchGrounding(t *testing.T) {
	t.Parallel()

	mockProv := mock.NewProvider()
	// Step 0: Search grounder answers the question set with verified facts
	mockProv.AddRule(mock.Rule{
		PromptPattern: "media intelligence research assistant",
		Response: SearchContext{
			SearchSummary: "GirlsWay is a lesbian adult entertainment website and production company.",
			DetectedType:  "Western adult video / porn",
			OfficialTitle: "Megan Mistakes Runaway Brides Regret",
			Studio:        "GirlsWay",
			Actors:        []string{"Khloe Kapri", "Megan Mistakes"},
			// The confirmed release date answer resolves the dotted-date order
			// ambiguity and grounds the year; the verified answers must survive
			// to downstream.
			ReleaseDate: "2026-09-06",
		},
	})
	// Checkers run with search context injected into payload
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Western/general adult video (porn)",
		Response: CheckerResponse{
			Confidence: ConfidenceYes,
			Reason:     "Confirmed Western adult video from search context (GirlsWay studio)",
			Entities: CheckerEntities{
				CleanTitle: "Megan Mistakes Runaway Brides Regret",
				Year:       2026,
				Actors:     []string{"Khloe Kapri", "Megan Mistakes"},
				// The VR fact learned from the web search grounding must flow
				// from the porn checker into the Stage 1 entities.
				IsVR: true,
			},
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "Japanese adult video (JAV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not JAV",
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "standalone movie or film",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not a mainstream movie",
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "episodic TV series",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not TV series",
		},
	})
	mockProv.AddRule(mock.Rule{
		PromptPattern: "music video (MV)",
		Response: CheckerResponse{
			Confidence: ConfidenceNo,
			Reason:     "Not music video",
		},
	})

	llm := NewClassifierLLM(mockProv)
	files := []string{"GirlsWay Khloe Kapri Megan Mistakes Runaway Brides Regret 2026 2160p WEB-DL H264 AAC2.0-VSEX.mp4"}

	res, err := llm.Classify(context.Background(), files, nil)
	require.NoError(t, err)
	assert.Equal(t, model.CategoryPorn, res.Category)
	assert.Equal(t, "Megan Mistakes Runaway Brides Regret", res.Entities["clean_title"])
	assert.Equal(t, 2026, res.Entities["year"])
	assert.Equal(t, true, res.Entities["is_vr"], "the web-search-grounded VR fact must flow into the entities")
	// Every verified grounder answer that later stages consume must survive
	// onto the Stage 1 entities (identity fields overlay the checker's echo).
	assert.Equal(t, "GirlsWay", res.Entities["studio"], "canonical studio must reach downstream")
	assert.Equal(t, "2026-09-06", res.Entities["release_date"], "confirmed release date must reach downstream")

	// Verify that the search grounder prompt was called in step 0
	calls := mockProv.Calls()
	searchCalled := false
	for _, call := range calls {
		if strings.Contains(call.Prompt, "media intelligence research assistant") {
			searchCalled = true
			break
		}
	}
	assert.True(t, searchCalled, "search grounder must be called during classification")
}

func TestMergeSearchFacts_Precedence(t *testing.T) {
	t.Parallel()

	// Checker/arbiter guessed some identity; the grounder verified others.
	base := entitiesToMap(CheckerEntities{
		CleanTitle: "Guessed Scene Title",
		Year:       2019,
		Actors:     []string{"Azul H."}, // abbreviation a checker echoed
		IsVR:       true,                // checker already grounded/flag true
	}, "reason")

	grounder := SearchContext{
		OfficialTitle: "Cheating Azul Needs DP Satisfaction",     // verified official wins
		Actors:        []string{"Azul Hermosa", "Chris Diamond"}, // full names win
		Studio:        "Tushy",
		ReleaseDate:   "2026-07-19", // derived year 2026 wins over the checker's 2019
		Series:        "",
		IsVR:          false, // search missed VR: must NOT clear the checker's true
		DetectedType:  "Western adult video / porn",
	}
	mergeSearchFacts(base, grounder)

	assert.Equal(t, "Cheating Azul Needs DP Satisfaction", base["clean_title"])
	assert.Equal(t, 2026, base["year"])
	assert.Equal(t, []string{"Azul Hermosa", "Chris Diamond"}, base["actors"])
	assert.Equal(t, "Tushy", base["studio"])
	assert.Equal(t, "2026-07-19", base["release_date"])
	assert.Equal(t, "Western adult video / porn", base["detected_type"])
	// A grounding false never downgrades an already-true VR fact.
	assert.Equal(t, true, base["is_vr"])
}

func TestMergeSearchFacts_KeepsGuessesWhenGrounderSilent(t *testing.T) {
	t.Parallel()

	base := entitiesToMap(CheckerEntities{
		CleanTitle: "Local Clean Title",
		Year:       2020,
	}, "reason")

	// An empty grounder (search unsupported / failed) changes nothing.
	mergeSearchFacts(base, SearchContext{})

	assert.Equal(t, "Local Clean Title", base["clean_title"])
	assert.Equal(t, 2020, base["year"])
	assert.NotContains(t, base, "studio")
	assert.NotContains(t, base, "release_date")
}
