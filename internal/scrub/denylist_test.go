package scrub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/wikidumptest"
	"github.com/stretchr/testify/require"
)

// The deny list holds digests of the lyric n-grams of the inline lyrics dropped from the committed scrubbed copy of
// the legacy wiki crawl (testdata/wiki-dump/denylist.txt): each window of 8 words of a dropped block, except
// windows that also occur outside the blocks. No text of the crawl can be read back from it.
const (
	denylistFile   = "denylist.txt"
	denylistWindow = 8
	digestBytes    = 16
	// denylistKey is a fixed, public salt: it defeats precomputed tables, not a targeted guess, which is acceptable
	// for lyric n-grams.
	denylistKey = "bardic-torch/denylist/v1"
)

// escapedBreak is a backslash escape of a line break or tab (in JSON or a Go string): it reads as a space, so that
// words on either side of an escaped line break are words.
var escapedBreak = regexp.MustCompile(`\\[nrt]`)

// word is a run of letters, digits and combining marks of a text, folded to lower case without accents, at
// text[start:end]. Apostrophes separate words.
type word struct {
	folded string
	start  int
	end    int
}

func words(text string) []word {
	var out []word
	var folded strings.Builder
	start := -1
	endWord := func(end int) {
		if start >= 0 {
			out = append(out, word{folded: folded.String(), start: start, end: end})
			folded.Reset()
			start = -1
		}
	}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) && start >= 0 {
			if start < 0 {
				start = i
			}
			folded.WriteString(foldRune(r))
		} else {
			endWord(i)
		}
		i += size
	}
	endWord(len(text))

	return out
}

func foldRune(r rune) string {
	if r < utf8.RuneSelf {
		return string(unicode.ToLower(r))
	}

	return domain.CollationKey(string(r))
}

func digest(key string) string {
	mac := hmac.New(sha256.New, []byte(denylistKey))
	mac.Write([]byte(key))

	return hex.EncodeToString(mac.Sum(nil)[:digestBytes])
}

// scan returns the byte offsets at which text holds a deny-listed n-gram.
func scan(denylist map[string]bool, text string) []int {
	var hits []int
	ws := words(escapedBreak.ReplaceAllString(text, "  "))
	for k := 0; k+denylistWindow <= len(ws); k++ {
		folded := make([]string, denylistWindow)
		for j := range folded {
			folded[j] = ws[k+j].folded
		}
		if denylist[digest(strings.Join(folded, " "))] {
			hits = append(hits, ws[k].start)
		}
	}

	return hits
}

func TestWords_FoldedAndLocated(t *testing.T) {
	text := "Ó Brien's  wren_PLACEHOLDER-x"

	ws := words(text)

	var folded []string
	for _, w := range ws {
		folded = append(folded, w.folded)
		require.NotEmpty(t, text[w.start:w.end])
	}
	require.Equal(t, []string{"o", "brien", "s", "wren", "placeholder", "x"}, folded)
	require.Equal(t, "PLACEHOLDER", text[ws[4].start:ws[4].end])
}

// TestRepositoryHasNoDenylistedText scans every text file of the repository, the scrubbed copy, tests, golden
// files and docs included, for the n-grams of the lyrics dropped from the scrubbed copy.
func TestRepositoryHasNoDenylistedText(t *testing.T) {
	file, err := os.ReadFile(filepath.Join(wikidumptest.RepositoryRoot(t), wikidumptest.ScrubbedDir, denylistFile))
	require.NoError(t, err)
	denylist := map[string]bool{}
	for _, line := range strings.Split(string(file), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			denylist[line] = true
		}
	}
	require.NotEmpty(t, denylist)

	for _, f := range repositoryTextFiles(t) {
		for _, start := range scan(denylist, f.text) {
			t.Errorf("%s: deny-listed lyric n-gram at byte %d", f.path, start)
		}
	}
}
