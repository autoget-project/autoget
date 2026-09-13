package stage1classifier

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

func TestMatchByRules_OrganizerCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		files        []string
		organizerCat interface{}
		wantCategory model.Category
		wantMatched  bool
	}{
		{
			// M8a: organizer_category = ["audio_book"] with pure mp3 files
			// must resolve to audio_book, not music.
			name:         "audio_book_list",
			files:        []string{"chapter01.mp3", "chapter02.mp3"},
			organizerCat: []string{"audio_book"},
			wantCategory: model.CategoryAudioBook,
			wantMatched:  true,
		},
		{
			name:         "single_string",
			files:        []string{"anything.mp4"},
			organizerCat: "book",
			wantCategory: model.CategoryBook,
			wantMatched:  true,
		},
		{
			name:         "invalid_item_before_valid",
			files:        []string{"video.mp4"},
			organizerCat: []string{"invalid_category_xyz", "bango_porn"},
			wantCategory: model.CategoryBangoPorn,
			wantMatched:  true,
		},
		{
			// Ambiguous multi-valued organizer_category (upstream source is
			// unsure) must NOT rule-match; it degrades to the LLM classifier.
			// Without this the first entry (bango_porn) would win and western
			// releases like GirlsWay end up skipped by the bango planner.
			name:         "ambiguous_multi_category_degrades_to_llm",
			files:        []string{"GirlsWay.26.09.06.Khloe.Kapri.XXX.2160p.mp4"},
			organizerCat: []string{"bango_porn", "porn"},
			wantMatched:  false,
		},
		{
			// Duplicate entries of the same category are not ambiguous.
			name:         "duplicate_single_category",
			files:        []string{"video.mp4"},
			organizerCat: []string{"porn", "porn"},
			wantCategory: model.CategoryPorn,
			wantMatched:  true,
		},
		{
			// All invalid items keep evaluating: the pure book check wins.
			name:         "all_invalid_items",
			files:        []string{"ebook.epub"},
			organizerCat: []string{"invalid_item_1", "invalid_item_2"},
			wantCategory: model.CategoryBook,
			wantMatched:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res, matched := MatchByRules(tt.files, map[string]interface{}{
				"organizer_category": tt.organizerCat,
			})
			require.Equal(t, tt.wantMatched, matched)
			assert.Equal(t, tt.wantCategory, res.Category)
		})
	}
}

func TestMatchByRules_DmmID(t *testing.T) {
	t.Parallel()

	res, matched := MatchByRules([]string{"random_name.mp4"}, map[string]interface{}{
		"dmm_id": "ssis00123",
	})
	require.True(t, matched, "dmm_id must match")
	assert.Equal(t, model.CategoryBangoPorn, res.Category)
	assert.Equal(t, "ssis00123", res.Entities["dmm_id"])
}

func TestMatchByRules_PureBook(t *testing.T) {
	t.Parallel()

	res, matched := MatchByRules([]string{"book1.epub", "folder/book2.pdf", "text.txt"}, nil)
	require.True(t, matched, "pure book extensions must match")
	assert.Equal(t, model.CategoryBook, res.Category)
}

func TestMatchByRules_PureAudioDegradation(t *testing.T) {
	t.Parallel()

	// M8b: pure audio files without metadata must NOT blindly match music;
	// they degrade to the LLM for disambiguation.
	_, matched := MatchByRules([]string{"track01.mp3", "track02.flac", "audio.m4a"}, nil)
	assert.False(t, matched)
}

func TestMatchByRules_StandardBangoRegex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filename  string
		wantBango string
	}{
		{"SSIS-001.mp4", "SSIS-001"},
		{"abp-123.mkv", "ABP-123"},
		{"FC2-PPV-123456.mp4", "FC2-PPV-123456"},
		{"FC2-1234567.mp4", "FC2-1234567"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			t.Parallel()

			res, matched := MatchByRules([]string{tt.filename}, nil)
			require.True(t, matched, "expected bango match for %s", tt.filename)
			assert.Equal(t, model.CategoryBangoPorn, res.Category)
			assert.Equal(t, tt.wantBango, res.Entities["bango"])
		})
	}
}

func TestMatchByRules_ComplexNoiseFallthrough(t *testing.T) {
	t.Parallel()

	// Release group noise or multiple video files without a bango pattern
	// must fall through to the LLM.
	files := []string{
		"[HDSky] The.Matrix.1999.1080p.BluRay.x264.mkv",
		"sample.mkv",
	}
	_, matched := MatchByRules(files, nil)
	assert.False(t, matched)
}
