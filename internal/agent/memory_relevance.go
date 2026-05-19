package agent

import (
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
		// No query words — return first maxResults memories.
		if maxResults > 0 && maxResults < len(memories) {
			return memories[:maxResults]
		}
		return memories
	}

	// Score each memory by word overlap with query.
	type scored struct {
		memory MemoryEntry
		score  int
	}
	scoredMemories := make([]scored, 0, len(memories))

	for _, m := range memories {
		score := scoreRelevance(m, queryWords)
		scoredMemories = append(scoredMemories, scored{memory: m, score: score})
	}

	// Sort by score descending (simple bubble sort for small N).
	for i := 0; i < len(scoredMemories); i++ {
		for j := i + 1; j < len(scoredMemories); j++ {
			if scoredMemories[j].score > scoredMemories[i].score {
				scoredMemories[i], scoredMemories[j] = scoredMemories[j], scoredMemories[i]
			}
		}
	}

	// Limit results.
	if maxResults > 0 && maxResults < len(scoredMemories) {
		scoredMemories = scoredMemories[:maxResults]
	}

	result := make([]MemoryEntry, len(scoredMemories))
	for i, s := range scoredMemories {
		result[i] = s.memory
	}
	return result
}

// scoreRelevance counts how many query words appear in the memory's tag or content.
func scoreRelevance(m MemoryEntry, queryWords []string) int {
	text := strings.ToLower(m.Tag + " " + m.Content)
	score := 0
	for _, word := range queryWords {
		if strings.Contains(text, word) {
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
