package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/ai"
	"github.com/autoget-project/organizer/internal/model"
	stage1classifier "github.com/autoget-project/organizer/internal/pipeline/stage1_classifier"
)

// TestE2E_CategorizerRuleBehavior covers Stage 1 rule classification:
// pure eBook extensions archive as book, standard bango patterns route to bango mover,
// and pure eBook bango name is treated as book.
func TestE2E_CategorizerRuleBehavior(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		want  map[string]model.PlanAction
	}{
		{
			name: "book_extensions",
			files: []string{
				"ebooks_hash/ebook.pdf",
				"ebooks_hash/novel.epub",
				"ebooks_hash/document.txt",
			},
			want: map[string]model.PlanAction{
				"ebooks_hash": wantMove("book/ebooks_hash"),
			},
		},
		{
			name:  "bango_fc2_case_insensitive",
			files: []string{"fc2-123456.mp4"},
			want: map[string]model.PlanAction{
				"fc2-123456.mp4": wantMove("jav/素人/FC2-123456.mp4"),
			},
		},
		{
			name:  "bango_standard_3char_3digit",
			files: []string{"ABC-123.mp4"},
			want: map[string]model.PlanAction{
				"ABC-123.mp4": wantMove("jav/素人/ABC-123.mp4"),
			},
		},
		{
			name:  "non_video_bango_names_are_books",
			files: []string{"ABC-123.pdf"},
			want: map[string]model.PlanAction{
				"ABC-123.pdf": wantMove("book/ABC-123.pdf"),
			},
		},
	}

	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
					Dir:   "dl",
					Files: tc.files,
				})
				assertPlanContract(t, code, body, tc.want)
			})
		}
	})
}

// TestE2E_CategorizerLLMDisambiguation tests dirty/ambiguous inputs that require LLM classification:
// audiobooks (chapter mp3s) vs western porn (no bango code).
func TestE2E_CategorizerLLMDisambiguation(t *testing.T) {
	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		t.Run("audiobook_disambiguation", func(t *testing.T) {
			files := []string{
				"The_Hobbit/Chapter_01.mp3",
				"The_Hobbit/Chapter_02.mp3",
			}
			code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
				Dir:   "audiobookdl",
				Files: files,
			})
			require.Equal(t, 200, code, body)
			var resp model.PlanResponse
			decodeBody(t, code, body, &resp)
			assert.Nil(t, resp.Error)
			require.NotEmpty(t, resp.Plan)

			// Invariant: should plan under audio_book
			for _, act := range resp.Plan {
				assert.Equal(t, "move", act.Action)
				require.NotNil(t, act.Target)
				assert.Contains(t, *act.Target, "audio_book/")
			}
		})

		t.Run("western_porn_disambiguation", func(t *testing.T) {
			files := []string{
				"Brazzers.Exxtra.Hot.Summer.Scenes.1080p.mp4",
			}
			code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
				Dir:   "porndl",
				Files: files,
			})
			require.Equal(t, 200, code, body)
			var resp model.PlanResponse
			decodeBody(t, code, body, &resp)
			assert.Nil(t, resp.Error)
			require.NotEmpty(t, resp.Plan)
			assert.Equal(t, "move", resp.Plan[0].Action)
			require.NotNil(t, resp.Plan[0].Target)
			// Invariant: should plan under porn/
			assert.Contains(t, *resp.Plan[0].Target, "porn/")
		})

		t.Run("western_porn_dotted_date_girlsway", func(t *testing.T) {
			files := []string{
				"GirlsWay.26.09.06.Khloe.Kapri.Megan.Mistakes.Runaway.Brides.Regret.XXX.2160p.mp4",
			}
			code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
				Dir:   "porndl",
				Files: files,
			})
			require.Equal(t, 200, code, body)
			var resp model.PlanResponse
			decodeBody(t, code, body, &resp)
			assert.Nil(t, resp.Error)
			require.NotEmpty(t, resp.Plan)
			assert.Equal(t, "move", resp.Plan[0].Action, "plan: %#v", resp.Plan)
			require.NotNil(t, resp.Plan[0].Target)
			assert.Contains(t, *resp.Plan[0].Target, "porn/")
		})
	})
}

// TestE2E_Stage1CheckersOnly executes ONLY Stage 1 Classifier (Rule -> Specialist Checkers -> Arbiter)
// across major domains without executing downstream stages, making it fast and easy to catch regressions.
func TestE2E_Stage1CheckersOnly(t *testing.T) {
	cases := []struct {
		name         string
		files        []string
		metadata     map[string]interface{}
		wantCategory model.Category
	}{
		{
			name: "western_adult_girlsway",
			files: []string{
				"GirlsWay Khloe Kapri Megan Mistakes Runaway Brides Regret 2026 2160p WEB-DL H264 AAC2.0-VSEX.mp4",
			},
			wantCategory: model.CategoryPorn,
		},
		{
			name: "western_adult_girlsway_dotted_date",
			files: []string{
				"GirlsWay.26.09.06.Khloe.Kapri.Megan.Mistakes.Runaway.Brides.Regret.XXX.2160p.mp4",
			},
			wantCategory: model.CategoryPorn,
		},
		{
			name: "western_adult_brazzers",
			files: []string{
				"Brazzers.Exxtra.Hot.Summer.Scenes.1080p.mp4",
			},
			wantCategory: model.CategoryPorn,
		},
		{
			name: "movie_noisy_release",
			files: []string{
				"[HDSky] Inception.2010.1080p.BluRay.x264.DTS-HD.MA.5.1-HDChina.mkv",
			},
			wantCategory: model.CategoryMovie,
		},
		{
			name: "tv_series_noisy_episode",
			files: []string{
				"[MTeam] Game.of.Thrones.S01E01.Winter.Is.Coming.1080p.BluRay.x264-ROVERS.mkv",
			},
			wantCategory: model.CategoryTVSeries,
		},
		{
			name: "jav_bango_dirty_release",
			files: []string{
				"[ThZu] ssis-00147 4K 1080p uncensored leaked.mp4",
			},
			wantCategory: model.CategoryBangoPorn,
		},
		{
			name: "audiobook_chapters",
			files: []string{
				"Three_Body_Problem/Chapter_01_Prologue.mp3",
				"Three_Body_Problem/Chapter_02_Silent_Spring.mp3",
			},
			wantCategory: model.CategoryAudioBook,
		},
		{
			name: "music_album_tracks",
			files: []string{
				"Taylor Swift - 1989/01. Welcome to New York.flac",
				"Taylor Swift - 1989/02. Blank Space.flac",
			},
			wantCategory: model.CategoryMusic,
		},
		{
			name: "photobook_image_set",
			files: []string{
				"Amami Tsubasa Photo Collection/001.jpg",
				"Amami Tsubasa Photo Collection/002.jpeg",
				"Amami Tsubasa Photo Collection/003.png",
			},
			wantCategory: model.CategoryPhotobook,
		},
		{
			name: "movie_with_subtitle_and_nfo",
			files: []string{
				"Inception.2010.1080p.BluRay.x264-HDChina.mkv",
				"Inception.2010.1080p.BluRay.x264-HDChina.chi.srt",
				"Inception.2010.nfo",
			},
			wantCategory: model.CategoryMovie,
		},
		{
			name: "unknown_software_installer",
			files: []string{
				"Photoshop_2024_Installer/setup.exe",
				"Photoshop_2024_Installer/assets/data01.rar",
			},
			wantCategory: model.CategoryUnknown,
		},
	}

	runWithLiveAIProviders(t, func(t *testing.T, prov ai.Provider) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				res, err := stage1classifier.ClassifyPipeline(t.Context(), prov, tc.files, tc.metadata)
				require.NoError(t, err, "Stage 1 classification must not return an error")
				assert.Equal(t, tc.wantCategory, res.Category, "expected category %s, got %s (reason: %v)", tc.wantCategory, res.Category, res.Entities["reason"])
			})
		}
	})
}
