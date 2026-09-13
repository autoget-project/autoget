package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestISO639ToLanguage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code     string
		expected Language
	}{
		{"zh", LanguageChinese},
		{"cn", LanguageChinese},
		{"chi", LanguageChinese},
		{"zho", LanguageChinese},
		{"mandarin", LanguageChinese},
		{"cantonese", LanguageChinese},
		{"chinese", LanguageChinese},
		{"en", LanguageEnglish},
		{"eng", LanguageEnglish},
		{"english", LanguageEnglish},
		{"ja", LanguageJapanese},
		{"jp", LanguageJapanese},
		{"jpn", LanguageJapanese},
		{"japanese", LanguageJapanese},
		{"ko", LanguageKorean},
		{"kor", LanguageKorean},
		{"korean", LanguageKorean},
		{"fr", LanguageOthers},
		{"de", LanguageOthers},
		{"unknown", LanguageOthers},
		{"", LanguageOthers},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, ISO639ToLanguage(tt.code))
		})
	}
}

func TestCategoryEnums(t *testing.T) {
	t.Parallel()

	assert.Len(t, AllCategories, 10)
	assert.Len(t, SimpleMoveCategories, 5)

	data, err := json.Marshal(CategoryMovie)
	require.NoError(t, err)
	assert.Equal(t, `"movie"`, string(data))

	var cat Category
	require.NoError(t, json.Unmarshal([]byte(`"tv_series"`), &cat))
	assert.Equal(t, CategoryTVSeries, cat)
}
