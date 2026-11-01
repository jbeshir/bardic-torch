package scrub

import (
	"sort"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/mediawiki"
)

// numberUsers numbers the wiki users of activity in a fixed order: pages by page ID (pages titled none last, by
// title), and each page's users by name.
func (s *scrubber) numberUsers(activity []mediawiki.PageActivity, pages []mediawiki.Page) {
	ids := map[string]int64{}
	for _, p := range pages {
		ids[p.Title] = p.PageID
	}
	var kept []mediawiki.PageActivity
	for _, a := range activity {
		if KeptNamespaces[a.NS] {
			kept = append(kept, a)
		}
	}
	sort.SliceStable(kept, func(a, b int) bool {
		idA, okA := ids[kept[a].Title]
		idB, okB := ids[kept[b].Title]
		if okA != okB {
			return okA
		}
		if idA != idB {
			return idA < idB
		}

		return kept[a].Title < kept[b].Title
	})
	for _, a := range kept {
		for _, name := range sortedKeys(a.Users) {
			s.user(name)
		}
	}
}

// user returns the placeholder of the wiki user name.
func (s *scrubber) user(name string) string {
	if startsWithPlaceholder(name) {
		return name
	}

	return numbered(s.users, mediawiki.NormTitle(name), wikiUserFormat)
}

// userTalkPrefix is the prefix of a user's talk page, a namespace links do not otherwise name.
const userTalkPrefix = "user talk:"

// userLinks replaces the user name of every link to a user page or user talk page (which is also what a
// signature is) with its placeholder, as the target and, if the link has one, as the display.
func (s *scrubber) userLinks(text string) string {
	var b strings.Builder
	last := 0
	for _, l := range mediawiki.FindWikilinks(text) {
		name, ok := userName(l)
		if !ok || startsWithPlaceholder(name) {
			continue
		}
		b.WriteString(text[last:l.Start])
		b.WriteString(userLinkPrefix(l.Raw))
		user := s.user(name)
		b.WriteString(user)
		if l.Piped {
			b.WriteString("|" + user)
		}
		b.WriteString("]]")
		last = l.End
	}
	b.WriteString(text[last:])

	return b.String()
}

// userName returns the user a link to a user page or user talk page names, without any subpage.
func userName(l mediawiki.Wikilink) (string, bool) {
	name := l.Target
	switch {
	case l.NS == mediawiki.NSUser:
	case strings.HasPrefix(strings.ToLower(name), userTalkPrefix):
		name = strings.TrimSpace(name[len(userTalkPrefix):])
	default:
		return "", false
	}
	if slash := strings.IndexByte(name, '/'); slash >= 0 {
		name = name[:slash]
	}

	return name, name != ""
}

// userLinkPrefix is a user link's text up to and including its namespace's colon: "[[User:", "[[:user talk:".
func userLinkPrefix(raw string) string {
	k := len("[[")
	for k < len(raw) && (raw[k] == ':' || raw[k] == ' ') {
		k++
	}

	return raw[:k+strings.IndexByte(raw[k:], ':')+1]
}
