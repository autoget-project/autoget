// Package ptr provides small pointer helpers shared across packages.
package ptr

// Str returns a pointer to the given string.
func Str(s string) *string { return &s }

// Float32 returns a pointer to the given float32.
func Float32(v float32) *float32 { return &v }
