package mediawiki

import (
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCategories = `{"generated":"x","categories":{"Bards":{"name":"Bards","page_exists":true,"hidden":false,` +
		`"parents":["Top"],"subcategories":[],"pages":[{"title":"Wren Placeholder","ns":0}],` +
		`"description_wikitext":"All bards."}}}`
	testRedirects = `{"allredirects_ns0":[],"redirect_pages":[{"from":"Wren P","ns":0,"to":"Wren Placeholder",` +
		`"fragment":null}]}`
	testActivity = `{"users":{},"per_page":[{"title":"Wren Placeholder","ns":0,"revisions":3,` +
		`"users":{"Example User":3},"first":"2024-01-02T03:04:05Z","last":"2025-01-02T03:04:05Z"}]}`
	pageRecord = `{"ns":0,"title":"Wren Placeholder","pageid":12,"categories":["Bards"],"hidden_categories":[],` +
		`"links":[],"extlinks":["https://example.org/"],"last_rev_timestamp":"2025-01-02T03:04:05Z",` +
		`"last_rev_user":"Example User","revid":99,"wikitext":"= Wren Placeholder =\n","url":"https://wiki.example/` +
		`Wren_Placeholder"}`
)

func dumpFS(pages string) fstest.MapFS {
	return fstest.MapFS{
		PagesFile:      {Data: []byte(pages)},
		CategoriesFile: {Data: []byte(testCategories)},
		RedirectsFile:  {Data: []byte(testRedirects)},
		ActivityFile:   {Data: []byte(testActivity)},
	}
}

func TestLoadDump_MapFS(t *testing.T) {
	d, err := LoadDump(dumpFS(pageRecord + "\n\n" + strings.Replace(pageRecord, `"pageid":12`, `"pageid":13`, 1)))
	require.NoError(t, err)

	require.Len(t, d.Pages, 2)
	p := d.Pages[0]
	assert.Equal(t, Page{
		NS: 0, Title: wren, PageID: 12, Categories: []string{CategoryBards}, HiddenCategories: []string{},
		Wikitext: "= Wren Placeholder =\n", URL: "https://wiki.example/Wren_Placeholder", RevID: 99,
		LastRevTimestamp: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	}, p)
	assert.True(t, p.HasCategory(CategoryBards))
	assert.False(t, p.HasCategory(CategoryPieces))
	assert.Equal(t, int64(13), d.Pages[1].PageID)
	assert.Equal(t, map[string]Category{CategoryBards: {
		Name: CategoryBards, PageExists: true, Parents: []string{"Top"}, Subcategories: []string{},
		Pages: []CategoryMember{{Title: wren}}, DescriptionWikitext: "All bards.",
	}}, d.Categories)
	assert.Equal(t, []Redirect{{From: "Wren P", To: wren}}, d.Redirects)
	assert.Equal(t, []PageActivity{{
		Title: wren, Revisions: 3, Users: map[string]int{"Example User": 3},
		First: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Last: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	}}, d.Activity)
}

func TestLoadDump_MalformedLine(t *testing.T) {
	for name, pages := range map[string]string{
		"not json":       pageRecord + "\n{not json\n",
		"two values":     pageRecord + " {}\n",
		"wrong type":     `{"pageid":"twelve"}` + "\n",
		"truncated last": pageRecord + "\n" + pageRecord[:40],
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadDump(dumpFS(pages))
			require.ErrorIs(t, err, ErrMalformedDump)
			assert.Contains(t, err.Error(), PagesFile+" line ")
			assert.NotContains(t, err.Error(), "Placeholder")
		})
	}
}

func TestLoadDump_MissingFile(t *testing.T) {
	for _, name := range []string{PagesFile, CategoriesFile, RedirectsFile, ActivityFile} {
		t.Run(name, func(t *testing.T) {
			fsys := dumpFS(pageRecord)
			delete(fsys, name)
			_, err := LoadDump(fsys)
			require.ErrorIs(t, err, ErrMalformedDump)
			assert.Contains(t, err.Error(), name)
		})
	}
}

func TestLoadDump_MalformedJSONFile(t *testing.T) {
	fsys := dumpFS(pageRecord)
	fsys[CategoriesFile] = &fstest.MapFile{Data: []byte(`{"categories": [}`)}
	_, err := LoadDump(fsys)
	require.ErrorIs(t, err, ErrMalformedDump)
	assert.Contains(t, err.Error(), CategoriesFile)
}

func TestLoadDump_PageRecordOverCap(t *testing.T) {
	huge := strings.Replace(pageRecord, `"wikitext":"`, `"wikitext":"`+strings.Repeat("a", MaxPageRecordBytes), 1)
	for name, pages := range map[string]string{
		"last line":   pageRecord + "\n" + huge,
		"middle line": huge + "\n" + pageRecord + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := LoadDump(dumpFS(pages))
			require.ErrorIs(t, err, ErrMalformedDump)
			assert.Contains(t, err.Error(), "line ")
			assert.Less(t, len(err.Error()), 200)
		})
	}

	justUnder := strings.Replace(pageRecord, `"wikitext":"`, `"wikitext":"`+
		strings.Repeat("a", MaxPageRecordBytes-len(pageRecord)-1), 1)
	d, err := LoadDump(dumpFS(justUnder + "\n"))
	require.NoError(t, err)
	assert.Len(t, d.Pages, 1)
}
