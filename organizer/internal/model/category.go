package model

import "strings"

// Category defines the media category classifications.
type Category string

const (
	CategoryUnknown    Category = "unknown"
	CategoryMovie      Category = "movie"
	CategoryTVSeries   Category = "tv_series"
	CategoryPhotobook  Category = "photobook"
	CategoryPorn       Category = "porn"
	CategoryBangoPorn  Category = "bango_porn"
	CategoryAudioBook  Category = "audio_book"
	CategoryBook       Category = "book"
	CategoryMusic      Category = "music"
	CategoryMusicVideo Category = "music_video"
)

// AllCategories lists all recognized Category values.
var AllCategories = []Category{
	CategoryUnknown,
	CategoryMovie,
	CategoryTVSeries,
	CategoryPhotobook,
	CategoryPorn,
	CategoryBangoPorn,
	CategoryAudioBook,
	CategoryBook,
	CategoryMusic,
	CategoryMusicVideo,
}

// SimpleMoveCategories lists categories handled by the simple mover logic.
var SimpleMoveCategories = []Category{
	CategoryPhotobook,
	CategoryAudioBook,
	CategoryBook,
	CategoryMusic,
	CategoryMusicVideo,
}

// Language defines language enums aligned with the system design.
type Language string

const (
	LanguageChinese  Language = "Chinese"
	LanguageEnglish  Language = "English"
	LanguageJapanese Language = "Japanese"
	LanguageKorean   Language = "Korean"
	LanguageOthers   Language = "Others"
)

// TargetDir defines standard root directories under TARGET_DIR.
type TargetDir string

const (
	TargetDirAudioBook    TargetDir = "audio_book"
	TargetDirBook         TargetDir = "book"
	TargetDirMusic        TargetDir = "music"
	TargetDirMusicVideo   TargetDir = "music_video"
	TargetDirPhotobook    TargetDir = "photobook"
	TargetDirMovie        TargetDir = "movie"
	TargetDirAnimMovie    TargetDir = "anim_movie"
	TargetDirTVSeries     TargetDir = "tv_series"
	TargetDirAnimTVSeries TargetDir = "anim_tv_series"
	TargetDirPorn         TargetDir = "porn"
	TargetDirPornVR       TargetDir = "porn_vr"
	TargetDirJAV          TargetDir = "jav"
	TargetDirJAVVR        TargetDir = "jav_vr"
	TargetDirMadou        TargetDir = "madou"
)

// AllTargetDirs lists all defined TargetDir values for directory validation.
var AllTargetDirs = []TargetDir{
	TargetDirAudioBook,
	TargetDirBook,
	TargetDirMusic,
	TargetDirMusicVideo,
	TargetDirPhotobook,
	TargetDirMovie,
	TargetDirAnimMovie,
	TargetDirTVSeries,
	TargetDirAnimTVSeries,
	TargetDirPorn,
	TargetDirPornVR,
	TargetDirJAV,
	TargetDirJAVVR,
	TargetDirMadou,
}

// Common media file extension sets.
var (
	VideoExtensions = map[string]struct{}{
		".mp4": {}, ".mkv": {}, ".avi": {}, ".mov": {}, ".wmv": {}, ".ts": {},
	}
	SubtitleExtensions = map[string]struct{}{
		".srt": {}, ".sub": {}, ".ass": {}, ".ssa": {}, ".vtt": {},
	}
)

// ISO639ToLanguage converts ISO 639-1 / 639-2 codes or common aliases to Language enum.
func ISO639ToLanguage(code string) Language {
	code = strings.TrimSpace(strings.ToLower(code))
	switch code {
	case "zh", "cn", "chi", "zho", "mandarin", "cantonese", "chinese":
		return LanguageChinese
	case "en", "eng", "english":
		return LanguageEnglish
	case "ja", "jp", "jpn", "japanese":
		return LanguageJapanese
	case "ko", "kor", "korean":
		return LanguageKorean
	default:
		return LanguageOthers
	}
}
