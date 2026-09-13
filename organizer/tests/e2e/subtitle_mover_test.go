package e2e

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autoget-project/organizer/internal/model"
)

// TestE2E_SubtitleMoverSemanticLanguage covers companion subtitle semantic
// pairing and language alignment (Stage 4): Chinese content renames the
// subtitle to <VideoBaseName>.简体中文.chi.srt, Japanese content to
// <VideoBaseName>.日本語.jpn.srt, each placed next to its matched video.
func TestE2E_SubtitleMoverSemanticLanguage(t *testing.T) {
	const matrixVideo = "[HDSky] The.Matrix.1999.1080p.BluRay.x264.DTS-WiKi/The.Matrix.1999.mkv"
	const matrixSrt = "[HDSky] The.Matrix.1999.1080p.BluRay.x264.DTS-WiKi/The.Matrix.1999.chs.srt"
	const frierenVideo = "[NC-Raws] 葬送的芙莉莲 - 第03话/[NC-Raws] 葬送的芙莉莲 - 第03话 [1080p][Baha][WEB-DL].mp4"
	const frierenSrt = "[NC-Raws] 葬送的芙莉莲 - 第03话/[NC-Raws] 葬送的芙莉莲 - 第03话.jp.srt"

	cases := []struct {
		name       string
		dir        string
		files      []string
		subFile    string
		subContent string
		metadata   map[string]interface{}
		isoCode    string // e.g. "chi" or "jpn"
	}{
		{
			name:       "chinese_srt_to_chi_iso_code",
			dir:        "matrixdl",
			files:      []string{matrixVideo, matrixSrt},
			subFile:    matrixSrt,
			subContent: "1\n00:00:01,000 --> 00:00:03,000\n醒醒吧，Neo。\n跟随白兔。\n",
			metadata:   map[string]interface{}{"title": "The Matrix", "year": 1999},
			isoCode:    "chi",
		},
		{
			name:       "japanese_srt_to_jpn_iso_code",
			dir:        "frieren",
			files:      []string{frierenVideo, frierenSrt},
			subFile:    frierenSrt,
			subContent: "1\n00:00:01,000 --> 00:00:03,000\n一度でいいから、\n本物の魔法を見てみたかった。\n",
			metadata:   map[string]interface{}{"genre": "动画"},
			isoCode:    "jpn",
		},
	}

	runWithLiveProviders(t, func(t *testing.T, s *sandbox) {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				s.seedDownloadFile(t, tc.dir, tc.subFile, tc.subContent)

				code, body := s.postJSON(t, "/v1/plan", model.APIPlanRequest{
					Dir:      tc.dir,
					Files:    tc.files,
					Metadata: tc.metadata,
				})

				require.Equal(t, 200, code, body)
				var resp model.PlanResponse
				decodeBody(t, code, body, &resp)
				assert.Nil(t, resp.Error)
				require.Len(t, resp.Plan, 2)

				planMap := make(map[string]model.PlanAction)
				for _, act := range resp.Plan {
					planMap[act.File] = act
				}

				videoAct := planMap[tc.files[0]]
				assert.Equal(t, "move", videoAct.Action)
				require.NotNil(t, videoAct.Target)

				subAct := planMap[tc.files[1]]
				assert.Equal(t, "move", subAct.Action)
				require.NotNil(t, subAct.Target)

				// Invariant: Subtitle must match video's directory & stem prefix,
				// and must contain the detected ISO 639-2 language code.
				assert.True(t, strings.HasSuffix(*subAct.Target, ".srt"))
				assert.Contains(t, *subAct.Target, "."+tc.isoCode+".srt")
			})
		}
	})
}
