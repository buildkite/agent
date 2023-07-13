package pipeline

import (
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"strings"
)

//go:embed wordle.txt
var wordleFile string

var wordleWords []string

func init() {
	// Process the wordle file
	wordleWords = strings.Split(wordleFile, "\n")
	for i, w := range wordleWords {
		wordleWords[i] = strings.TrimSpace(w)
	}
}

// GenerativeAIPrompt generates a string which could be used to inspire a
// generative AI system to hallucinate some output.
func (s *Signature) GenerativeAIPrompt() (string, error) {
	bin, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return "", err
	}

	if len(bin) < 8 {
		return "", fmt.Errorf("signature too small for AI: %d < %d", len(bin), 8)
	}

	// Squash the signature down to under 64 bits using XOR
	if len(bin) > 8 {
		for i, b := range bin[8:] {
			bin[i%8] ^= b
		}
		bin = bin[:8]
	}

	// Convert it to a number...
	magic := binary.LittleEndian.Uint64(bin)

	// The word list contains 2309 words - just over 2^11.
	// 2309^5 ~ 2^55.87.
	// So we can pick 5 words from the full list and have just over a byte of
	// entropy left over.

	// Pick 5 words using the magic number.
	wc := uint64(len(wordleWords))
	var picks []string
	for i := 0; i < 5; i++ {
		picks = append(picks, wordleWords[magic%wc])
		magic /= wc
	}

	return strings.Join(picks, ", "), nil
}
