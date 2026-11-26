package stage1classifier

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPromptFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n        int
		wantLen  int
		wantNote string
	}{
		{0, 0, ""},
		{200, 200, ""},
		{201, 201, "(1 more files not shown)"},
		{2000, 201, "(1800 more files not shown)"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.n), func(t *testing.T) {
			t.Parallel()

			files := make([]string, tt.n)
			for i := range files {
				files[i] = fmt.Sprintf("f%d.jpg", i)
			}
			got := promptFiles(files)
			assert.Len(t, got, tt.wantLen)
			if tt.wantNote != "" {
				assert.Equal(t, tt.wantNote, got[len(got)-1])
				assert.Equal(t, "f199.jpg", got[199])
			}
		})
	}
}
