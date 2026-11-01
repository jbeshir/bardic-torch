// Package mediawiki decodes a crawl of the legacy Bardic Torch MediaWiki site and parses its wikitext. It is pure:
// every function takes values (or an fs.FS) and returns new values, and nothing here writes anywhere.
//
// The parsing functions port the research scripts (wikitext.mjs and the page analysis of analyze.mjs), which are
// the reference behaviour; where this package deliberately differs, the research oracle test lists why
// (oracleDeviations).
package mediawiki

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"time"
)

// ErrMalformedDump is returned when a dump file is missing, is not the JSON the crawler writes, or holds a page
// record over MaxPageRecordBytes. Its messages name the file and line, never the content.
var ErrMalformedDump = errors.New("malformed wiki dump")

// MaxPageRecordBytes caps one encoded page record (one line of pages.jsonl). The longest real page is under
// 50 KB, so a record over 2 MiB is not a page the crawler wrote.
const MaxPageRecordBytes = 2 << 20

// The files of a dump that LoadDump reads.
const (
	PagesFile      = "pages.jsonl"
	CategoriesFile = "categories.json"
	RedirectsFile  = "redirects.json"
	ActivityFile   = "activity.json"
)

// Dump is the part of a crawl the importer uses.
type Dump struct {
	Pages      []Page
	Categories map[string]Category
	Redirects  []Redirect
	Activity   []PageActivity
}

// Page is one page of the wiki: its identity, categories and current wikitext.
type Page struct {
	NS               int       `json:"ns"`
	Title            string    `json:"title"`
	PageID           int64     `json:"pageid"`
	Categories       []string  `json:"categories"`
	HiddenCategories []string  `json:"hidden_categories"`
	Wikitext         string    `json:"wikitext"`
	URL              string    `json:"url"`
	Redirect         bool      `json:"redirect"`
	RevID            int64     `json:"revid"`
	LastRevTimestamp time.Time `json:"last_rev_timestamp"`
}

// HasCategory reports whether the page is a member of the category name.
func (p Page) HasCategory(name string) bool {
	for _, c := range p.Categories {
		if c == name {
			return true
		}
	}

	return false
}

// Category is one category of the wiki, whether or not it has a page.
type Category struct {
	Name                string           `json:"name"`
	PageExists          bool             `json:"page_exists"`
	Parents             []string         `json:"parents"`
	Subcategories       []string         `json:"subcategories"`
	Pages               []CategoryMember `json:"pages"`
	DescriptionWikitext string           `json:"description_wikitext"`
}

// CategoryMember is a page in a category.
type CategoryMember struct {
	Title string `json:"title"`
	NS    int    `json:"ns"`
}

// Redirect is a redirect page in namespace NS (0 for pages, 14 for categories): From redirects to To.
type Redirect struct {
	From string `json:"from"`
	NS   int    `json:"ns"`
	To   string `json:"to"`
}

// PageActivity is the edit history of one page: its revision count, the revisions per user and the first and
// last revision times.
type PageActivity struct {
	Title     string         `json:"title"`
	NS        int            `json:"ns"`
	Revisions int            `json:"revisions"`
	Users     map[string]int `json:"users"`
	First     time.Time      `json:"first"`
	Last      time.Time      `json:"last"`
}

// LoadDump reads the dump in fsys: pages.jsonl (one page per line), categories.json, redirects.json and
// activity.json (only its per-page activity). Fields the importer does not use are not decoded.
func LoadDump(fsys fs.FS) (Dump, error) {
	pages, err := loadPages(fsys)
	if err != nil {
		return Dump{}, err
	}
	var categories struct {
		Categories map[string]Category `json:"categories"`
	}
	if err := loadJSON(fsys, CategoriesFile, &categories); err != nil {
		return Dump{}, err
	}
	var redirects struct {
		RedirectPages []Redirect `json:"redirect_pages"`
	}
	if err := loadJSON(fsys, RedirectsFile, &redirects); err != nil {
		return Dump{}, err
	}
	var activity struct {
		PerPage []PageActivity `json:"per_page"`
	}
	if err := loadJSON(fsys, ActivityFile, &activity); err != nil {
		return Dump{}, err
	}

	return Dump{
		Pages:      pages,
		Categories: categories.Categories,
		Redirects:  redirects.RedirectPages,
		Activity:   activity.PerPage,
	}, nil
}

func loadPages(fsys fs.FS) ([]Page, error) {
	f, err := fsys.Open(PagesFile)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrMalformedDump, PagesFile, err)
	}
	defer func() { _ = f.Close() }()

	// The reader's buffer holds one whole record, so a longer one is ErrBufferFull rather than unbounded memory.
	r := bufio.NewReaderSize(f, MaxPageRecordBytes+1)
	var pages []Page
	for line := 1; ; line++ {
		record, err := r.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("%w: %s line %d: record over %d bytes", ErrMalformedDump, PagesFile, line,
				MaxPageRecordBytes)
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: %s line %d: %w", ErrMalformedDump, PagesFile, line, err)
		}
		if len(record) > MaxPageRecordBytes {
			return nil, fmt.Errorf("%w: %s line %d: record over %d bytes", ErrMalformedDump, PagesFile, line,
				MaxPageRecordBytes)
		}
		if !isBlank(record) {
			page, decodeErr := decodePage(record)
			if decodeErr != nil {
				return nil, fmt.Errorf("%w: %s line %d: %w", ErrMalformedDump, PagesFile, line, decodeErr)
			}
			pages = append(pages, page)
		}
		if errors.Is(err, io.EOF) {
			return pages, nil
		}
	}
}

// decodePage decodes one record, which must hold exactly one JSON object.
func decodePage(record []byte) (Page, error) {
	dec := json.NewDecoder(bytes.NewReader(record))
	var page Page
	if err := dec.Decode(&page); err != nil {
		return Page{}, err
	}
	if dec.More() {
		return Page{}, errTrailingData
	}

	return page, nil
}

var errTrailingData = errors.New("more than one JSON value")

func loadJSON(fsys fs.FS, name string, v any) error {
	f, err := fsys.Open(name)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformedDump, name, err)
	}
	defer func() { _ = f.Close() }()

	dec := json.NewDecoder(f)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMalformedDump, name, err)
	}
	if dec.More() {
		return fmt.Errorf("%w: %s: %w", ErrMalformedDump, name, errTrailingData)
	}

	return nil
}

func isBlank(b []byte) bool {
	for _, c := range b {
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
	}

	return true
}
