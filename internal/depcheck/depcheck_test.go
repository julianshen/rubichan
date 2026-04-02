package depcheck

import "testing"

func TestLooksLikeDependencyCommandIntentRecognizesCommonPatterns(t *testing.T) {
	tests := []struct {
		command string
		want    bool
	}{
		{"npm install express", true},
		{"npm i express", true},
		{"pnpm install", true},
		{"pnpm i", true},
		{"yarn add lodash", true},
		{"pip install requests", true},
		{"go mod tidy", true},
		{"go get github.com/stretchr/testify", true},
		{"mvn install", true},
		{"cargo add serde", false},
		{"echo hello", false},
		{"", false},
	}
	for _, tt := range tests {
		got := LooksLikeDependencyCommandIntent(tt.command)
		if got != tt.want {
			t.Errorf("LooksLikeDependencyCommandIntent(%q) = %v, want %v", tt.command, got, tt.want)
		}
	}
}
