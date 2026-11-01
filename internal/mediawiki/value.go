package mediawiki

import (
	"regexp"
	"strings"
)

// ValueItemType is what one item of an infobox value is.
type ValueItemType string

// The infobox value item types.
const (
	ValuePageLink     ValueItemType = "page_link"
	ValueCategoryLink ValueItemType = "category_link"
	ValueCategoryTag  ValueItemType = "category_tag"
	ValueInterwiki    ValueItemType = "interwiki"
	ValueExternalLink ValueItemType = "external_link"
	ValueText         ValueItemType = "text"
)

// Value is an infobox parameter value split into items. Raw is the value without comments, trimmed. Separators
// are the separators between items in order (", ; / & and with <br>", lower case). FreeText joins the text items
// with " | ". Placeholder reports a template placeholder such as TagGoesHere.
type Value struct {
	Raw         string
	Items       []ValueItem
	Separators  []string
	FreeText    string
	Placeholder bool
}

// Empty reports whether the value has no content.
func (v Value) Empty() bool {
	return v.Raw == ""
}

// ValueItem is one item of a value. Links have a Target (a normalised title; an interwiki target as written) and
// may have a Display; external links have a URL. Label is what the item shows. An item inside parentheses after
// another item qualifies it: Qualifies is the qualified item's label and Depth the number of open parentheses,
// at most MaxNestingDepth.
type ValueItem struct {
	Type      ValueItemType
	Target    string
	Display   string
	URL       string
	Label     string
	Qualifies string
	Depth     int
}

var (
	valueToken = regexp.MustCompile(`(?i)\[\[[^\[\]]*\]\]|\[(?:https?:)?//[^\]]*\]|\(|\)|,|;|/|&|\band\b|\bwith\b|` +
		`<br[` + jsSpaces + `]*/?>`)
	valueEntity      = regexp.MustCompile(`^&\w+;`)
	valuePlaceholder = regexp.MustCompile(`(?i)TagGoesHere|TagNameHere|BardNameHere`)
	punctuationOnly  = regexp.MustCompile(`^[.\-–—:]+$`)
)

type valueTokenKind int

const (
	tokenText valueTokenKind = iota
	tokenWikilink
	tokenExternal
	tokenOpen
	tokenClose
	tokenSeparator
)

type valueTokenized struct {
	kind valueTokenKind
	text string
}

// ParseValue splits an infobox value into links, external links and text, at the separators outside links, and
// marks the items inside parentheses as qualifying the item before them, as the reference's parseValue does.
func ParseValue(raw string) Value {
	v := trimSpace(StripComments(raw))
	res := Value{Raw: v, Placeholder: valuePlaceholder.MatchString(v)}
	if v == "" {
		return res
	}
	depth := 0
	var parent *ValueItem
	var free []string
	for _, tk := range tokenizeValue(v) {
		switch tk.kind {
		case tokenOpen:
			depth++

			continue
		case tokenClose:
			depth = max(0, depth-1)

			continue
		case tokenSeparator:
			res.Separators = append(res.Separators, tk.text)

			continue
		case tokenText, tokenWikilink, tokenExternal:
		}
		item, ok := valueItem(tk)
		if !ok {
			continue
		}
		if item.Type == ValueText {
			free = append(free, item.Label)
		}
		if depth > 0 && parent != nil {
			item.Qualifies = parent.Label
			item.Depth = min(depth, MaxNestingDepth)
			res.Items = append(res.Items, item)

			continue
		}
		res.Items = append(res.Items, item)
		parent = &item
	}
	res.FreeText = strings.Join(free, " | ")

	return res
}

func tokenizeValue(v string) []valueTokenized {
	var tokens []valueTokenized
	pushText := func(t string) {
		if t = trimSpace(t); t != "" {
			tokens = append(tokens, valueTokenized{kind: tokenText, text: t})
		}
	}
	last := 0
	for _, m := range valueToken.FindAllStringIndex(v, -1) {
		s := v[m[0]:m[1]]
		if s == "&" && valueEntity.MatchString(v[m[0]:]) {
			continue
		}
		pushText(v[last:m[0]])
		last = m[1]
		lower := strings.ToLower(s)
		switch {
		case strings.HasPrefix(s, "[["):
			tokens = append(tokens, valueTokenized{kind: tokenWikilink, text: s})
		case strings.HasPrefix(s, "["):
			tokens = append(tokens, valueTokenized{kind: tokenExternal, text: s})
		case s == "(":
			tokens = append(tokens, valueTokenized{kind: tokenOpen})
		case s == ")":
			tokens = append(tokens, valueTokenized{kind: tokenClose})
		case strings.HasPrefix(lower, "<br"):
			tokens = append(tokens, valueTokenized{kind: tokenSeparator, text: "<br>"})
		default:
			tokens = append(tokens, valueTokenized{kind: tokenSeparator, text: lower})
		}
	}
	pushText(v[last:])

	return tokens
}

func valueItem(tk valueTokenized) (ValueItem, bool) {
	switch tk.kind {
	case tokenWikilink:
		l := FindWikilinks(tk.text)[0]
		item := ValueItem{Type: ValuePageLink, Target: l.Target, Display: l.Display, Label: l.Label()}
		switch l.Kind {
		case WikilinkCategoryLink:
			item.Type = ValueCategoryLink
		case WikilinkCategoryTag:
			item.Type = ValueCategoryTag
		case WikilinkInterwiki:
			item.Type = ValueInterwiki
		case WikilinkPage, WikilinkFile, WikilinkFileLink, WikilinkNamespace:
		}

		return item, true
	case tokenExternal:
		item := ValueItem{Type: ValueExternalLink}
		if links := FindExternalLinks(tk.text); len(links) > 0 {
			item.URL = links[0].URL
			item.Label = links[0].URL
			if links[0].Text != "" {
				item.Label = DecodeEntities(links[0].Text)
			}
		}

		return item, true
	case tokenText, tokenOpen, tokenClose, tokenSeparator:
	}
	txt := trimSpace(DecodeEntities(boldItalic.ReplaceAllString(tk.text, "")))
	if txt == "" || punctuationOnly.MatchString(txt) {
		return ValueItem{}, false
	}

	return ValueItem{Type: ValueText, Label: txt}, true
}
