package agent

import (
	"sort"
	"strings"
	"unicode"
)

// SelectRelevantMemories filters memories based on relevance to a query string
// (typically the user's message). It uses simple keyword overlap scoring:
// memories that share words with the query are ranked higher.
//
// Returns up to maxResults memories, sorted by relevance (highest first).
// If maxResults <= 0, returns all memories sorted by relevance.
func SelectRelevantMemories(memories []MemoryEntry, query string, maxResults int) []MemoryEntry {
	if len(memories) == 0 {
		return nil
	}

	queryWords := extractWords(query)
	if len(queryWords) == 0 {
		if maxResults > 0 && maxResults < len(memories) {
			return memories[:maxResults]
		}
		return memories
	}

	type scored struct {
		memory MemoryEntry
		score  int
	}
	scoredMemories := make([]scored, 0, len(memories))

	for _, m := range memories {
		scoredMemories = append(scoredMemories, scored{
			memory: m,
			score:  scoreRelevance(m.Normalized, queryWords),
		})
	}

	sort.Slice(scoredMemories, func(i, j int) bool {
		return scoredMemories[i].score > scoredMemories[j].score
	})

	if maxResults > 0 && maxResults < len(scoredMemories) {
		scoredMemories = scoredMemories[:maxResults]
	}

	result := make([]MemoryEntry, len(scoredMemories))
	for i, s := range scoredMemories {
		result[i] = s.memory
	}
	return result
}

// scoreRelevance counts how many query words appear in the normalized text.
func scoreRelevance(normalized string, queryWords []string) int {
	score := 0
	for _, word := range queryWords {
		if strings.Contains(normalized, word) {
			score++
		}
	}
	return score
}

// extractWords splits text into lowercase words, filtering out very short words.
func extractWords(text string) []string {
	var words []string
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		word = strings.ToLower(word)
		if len(word) > 2 {
			words = append(words, word)
		}
	}
	return words
}
