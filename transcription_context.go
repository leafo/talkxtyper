package main

import (
	"regexp"
	"strings"
	"unicode"
)

const maxTranscriptionKeywords = 100
const maxTranscriptionKeywordLength = 100

var transcriptionKeywordToken = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_./:@%+#-]*`)

type TranscriptionContext struct {
	Prompt    string
	Keywords  []string
	Languages []string
}

func NewTranscriptionContext(prompt string) TranscriptionContext {
	return TranscriptionContext{
		Prompt:    prompt,
		Keywords:  buildTranscriptionKeywords(config.Keywords, prompt),
		Languages: []string{"en"},
	}
}

func extractTranscriptionKeywords(prompt string) []string {
	return buildTranscriptionKeywords(nil, prompt)
}

// buildTranscriptionKeywords combines the configured seed keywords with terms
// extracted from the context prompt. Seeds come first so they survive the
// keyword cap; duplicates are dropped case-insensitively.
func buildTranscriptionKeywords(seed []string, prompt string) []string {
	keywords := make([]string, 0, maxTranscriptionKeywords)
	seen := make(map[string]struct{}, maxTranscriptionKeywords)

	add := func(keyword string) {
		if len(keywords) >= maxTranscriptionKeywords {
			return
		}

		keyword = sanitizeTranscriptionKeyword(keyword)
		if keyword == "" {
			return
		}

		key := strings.ToLower(keyword)
		if _, ok := seen[key]; ok {
			return
		}
		for _, existing := range keywords {
			for _, part := range strings.FieldsFunc(existing, func(r rune) bool {
				return !unicode.IsLetter(r) && !unicode.IsDigit(r)
			}) {
				if strings.EqualFold(part, keyword) {
					return
				}
			}
		}
		seen[key] = struct{}{}
		keywords = append(keywords, keyword)
	}

	for _, keyword := range seed {
		add(keyword)
	}

	// Screenshot analysis emits an explicit list after a "Keywords:" heading.
	// Preserve those phrases, including ordinary names that a token heuristic
	// cannot reliably distinguish from prose.
	inKeywordList := false
	for _, line := range strings.Split(prompt, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "keywords:") {
			inKeywordList = true
			continue
		}
		if inKeywordList {
			if strings.HasPrefix(trimmed, "- ") {
				add(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
				continue
			}
			inKeywordList = false
		}
	}

	for _, token := range transcriptionKeywordToken.FindAllString(prompt, -1) {
		if isTechnicalKeyword(token) {
			add(token)
		}
	}

	return keywords
}

func sanitizeTranscriptionKeyword(keyword string) string {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.TrimPrefix(keyword, "- ")
	keyword = strings.TrimSpace(keyword)
	keyword = strings.TrimRight(keyword, ".,;!?")

	if keyword == "" || len(keyword) > maxTranscriptionKeywordLength || strings.ContainsAny(keyword, "<>\r\n") {
		return ""
	}
	return keyword
}

func isTechnicalKeyword(token string) bool {
	if len(token) < 2 {
		return false
	}
	if strings.EqualFold(strings.TrimSuffix(token, ":"), "keywords") {
		return false
	}

	hasLetter := false
	hasDigit := false
	hasLower := false
	hasUpper := false
	upperCount := 0
	hasLowerToUpper := false
	var previous rune

	for _, r := range token {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
			if unicode.IsLower(r) {
				hasLower = true
			}
			if unicode.IsUpper(r) {
				hasUpper = true
				upperCount++
				if unicode.IsLower(previous) {
					hasLowerToUpper = true
				}
			}
		case unicode.IsDigit(r):
			hasDigit = true
		}
		previous = r
	}

	if !hasLetter {
		return false
	}
	if strings.ContainsAny(token, "_./:@%+#-") || hasDigit || hasLowerToUpper {
		return true
	}
	return (hasUpper && !hasLower) || (upperCount >= 2 && hasLower)
}

// parseKeywordList splits user-entered text into keywords, one per line or
// comma-separated, dropping blanks and duplicates.
func parseKeywordList(text string) []string {
	keywords := make([]string, 0)
	seen := make(map[string]struct{})
	for _, field := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == ',' }) {
		keyword := sanitizeTranscriptionKeyword(field)
		if keyword == "" {
			continue
		}
		key := strings.ToLower(keyword)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		keywords = append(keywords, keyword)
	}
	return keywords
}
