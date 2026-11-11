// Package scrub holds the lyric deny list of the committed scrubbed copy of the legacy wiki crawl (Denylist):
// digests of the lyric n-grams of the inline lyrics dropped from it. TestRepositoryHasNoDenylistedText scans every
// text file of the repository for them, so that no lyric is ever committed.
package scrub

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// DenylistWindow is the number of words of each lyric n-gram the deny list holds.
const DenylistWindow = 8

// DenylistFile is the deny list's file in the scrubbed copy.
const DenylistFile = "denylist.txt"

// denylistKey keys the digests. It is a fixed, public salt, so the list is reproducible: it defeats precomputed
// tables, not a targeted guess, which is acceptable for lyric n-grams.
const denylistKey = "bardic-torch/denylist/v1"

// denylistHeader is the first lines of the deny list file.
const denylistHeader = "# hmac-sha256/128 v1\n# window: 8 words\n"

// digestBytes is the length of a digest: the first 16 bytes of the HMAC.
const digestBytes = 16

// ErrMalformedDenylist is returned for a deny list file that is not a deny list.
var ErrMalformedDenylist = errors.New("malformed deny list")

// Denylist is the set of digests of the lyric n-grams the scrubber dropped: each window of DenylistWindow words
// (as mediawiki.Words reads them) of a dropped block, except windows that also occur outside the blocks, which
// the scrubbed copy holds legitimately (titles, quoted commentary). No text of the crawl can be read back from it.
type Denylist struct {
	digests map[string]bool
}

// Hit is where a scanned text holds a deny-listed n-gram: text[Start:End].
type Hit struct {
	Start int
	End   int
}

// Len is the number of digests.
func (d Denylist) Len() int {
	return len(d.digests)
}

// ParseDenylist reads a deny list file.
func ParseDenylist(file []byte) (Denylist, error) {
	if !bytes.HasPrefix(file, []byte(denylistHeader)) {
		return Denylist{}, fmt.Errorf("%w: unknown header", ErrMalformedDenylist)
	}
	d := Denylist{digests: map[string]bool{}}
	lines := bufio.NewScanner(bytes.NewReader(file[len(denylistHeader):]))
	for n := 1; lines.Scan(); n++ {
		line := lines.Text()
		if b, err := hex.DecodeString(line); err != nil || len(b) != digestBytes {
			return Denylist{}, fmt.Errorf("%w: digest line %d", ErrMalformedDenylist, n)
		}
		d.digests[line] = true
	}

	return d, nil
}

// escapedBreak is a backslash escape of a line break or tab (in JSON or a Go string): Scan reads it as a space, so
// that words on either side of an escaped line break are words.
var escapedBreak = regexp.MustCompile(`\\[nrt]`)

// Scan returns where text holds a deny-listed n-gram, in order. It never returns the n-gram itself.
func (d Denylist) Scan(text string) []Hit {
	var hits []Hit
	windows(escapedBreak.ReplaceAllString(text, "  "), func(key string, start, end int) {
		if d.digests[digest(key)] {
			hits = append(hits, Hit{Start: start, End: end})
		}
	})

	return hits
}

// windows calls f with each window of DenylistWindow words of text: its folded words joined by spaces, and where it
// starts and ends.
func windows(text string, f func(key string, start, end int)) {
	words := mediawiki.Words(text)
	folded := make([]string, len(words))
	for k, w := range words {
		folded[k] = w.Folded
	}
	for k := 0; k+DenylistWindow <= len(words); k++ {
		f(strings.Join(folded[k:k+DenylistWindow], " "), words[k].Start, words[k+DenylistWindow-1].End)
	}
}

func digest(key string) string {
	mac := hmac.New(sha256.New, []byte(denylistKey))
	mac.Write([]byte(key))

	return hex.EncodeToString(mac.Sum(nil)[:digestBytes])
}
