package e2e

import (
	"testing"

	"github.com/autoget-project/organizer/internal/model"
)

// TestE2E_SimpleMoverBranches covers the three-branch archiving strategy of
// the simple mover (local planner) through POST /v1/plan:
//  1. a single file moves under the category root;
//  2. multiple files directly under the hash dir move as a whole directory;
//  3. multiple files in sub dirs move per sub dir with the hash layer stripped.
func TestE2E_SimpleMoverBranches(t *testing.T) {
	cases := []struct {
		name     string
		files    []string
		metadata map[string]interface{}
		want     map[string]model.PlanAction
	}{
		{
			name:     "single_file",
			files:    []string{"torrent_hash/movie.mp4"},
			metadata: map[string]interface{}{"organizer_category": "music_video"},
			want: map[string]model.PlanAction{
				"torrent_hash/movie.mp4": wantMove("music_video/movie.mp4"),
			},
		},
		{
			name:     "multiple_files_under_hash_dir",
			files:    []string{"torrent_hash/episode1.mp4", "torrent_hash/episode2.mp4"},
			metadata: map[string]interface{}{"organizer_category": "music_video"},
			want: map[string]model.PlanAction{
				"torrent_hash": wantMove("music_video/torrent_hash"),
			},
		},
		{
			// Branch 3, hash layer stripped. Pure eBook extensions hit the
			// offline Stage 1 book rule, so no LLM is involved at all.
			name: "multiple_files_in_subdirs",
			files: []string{
				"torrent_hash/chapter1/page1.pdf",
				"torrent_hash/chapter1/page2.pdf",
				"torrent_hash/chapter2/page1.pdf",
			},
			metadata: nil,
			want: map[string]model.PlanAction{
				"torrent_hash/chapter1": wantMove("book/chapter1"),
				"torrent_hash/chapter2": wantMove("book/chapter2"),
			},
		},
		{
			// Branch 2 wins over branch 3 when any file sits directly in the
			// hash dir.
			name:     "mixed_files_and_dirs_under_hash_dir",
			files:    []string{"torrent_hash/song.mp3", "torrent_hash/album_art/cover.jpg"},
			metadata: map[string]interface{}{"organizer_category": "music"},
			want: map[string]model.PlanAction{
				"torrent_hash": wantMove("music/torrent_hash"),
			},
		},
	}

	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
					Dir:      "dl",
					Files:    tc.files,
					Metadata: tc.metadata,
				})
				assertPlanContract(t, code, body, tc.want)
			})
		}
	})
}
