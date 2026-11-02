package mediawiki

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testMaxYear = 2026

type performanceSummary struct {
	Uploader   string
	Platform   string
	Performers []string
	Event      string
	Year       int
	Piece      string
}

func summarise(item string) performanceSummary {
	p := ParsePerformanceText(item, testMaxYear)
	s := performanceSummary{Uploader: p.Uploader.Text, Platform: p.Platform, Event: p.Event.Text, Year: p.Year,
		Piece: p.Piece}
	for _, l := range p.Performers {
		s.Performers = append(s.Performers, l.Target)
	}
	if p.Uploader.Text != "" && item[p.Uploader.Start:p.Uploader.End] != p.Uploader.Text {
		s.Uploader = "span mismatch"
	}

	return s
}

func TestParsePerformanceText_FullGrammar(t *testing.T) {
	tests := map[string]performanceSummary{
		"[https://example.org/v/2014/1 Recording by Wren Placeholder on YouTube] of [[Ash Placeholder]] singing " +
			"[[A Song]] at Pennsic War, 2019": {
			Uploader: wren, Platform: youTube, Performers: []string{ash},
			Event: pennsicWar, Year: 2019, Piece: aSong,
		},
		"[https://example.org/v A Song, recorded by WrenSings on YouTube] performed by [[Ash Placeholder]] and " +
			"[[Rowan Placeholder]] at the [[" + greenDragon + "]], 2010": {
			Uploader: "WrenSings", Platform: youTube, Performers: []string{ash, rowan},
			Event: greenDragon, Year: 2010,
		},
		"[https://example.org/v A Song, uploaded to YouTube by Wren Placeholder, sung in choir in 2010]": {
			Uploader: wren, Platform: youTube, Year: 2010,
		},
		"[https://example.org/v A Song, recorded for YouTube by Wren Placeholder] sung by [[Ash Placeholder]]": {
			Uploader: wren, Platform: youTube, Performers: []string{ash},
		},
		"[https://example.org/v A Song on YouTube by 'Wren Placeholder'] at Twelfth Night 2012": {
			Uploader: wren, Platform: youTube, Event: "Twelfth Night", Year: 2012,
		},
		"[https://example.org/v A Song by Wren Placeholder on BandCamp]": {
			Uploader: wren, Platform: "BandCamp",
		},
		"[https://example.org/v A Song] recorded by Wren Placeholder, at Crown Tourney 1999": {
			Uploader: wren, Event: "Crown Tourney", Year: 1999,
		},
		"[https://example.org/v A Song] performed by [[Ash Placeholder]] to a tune by [[Rowan Placeholder]]": {
			Performers: []string{ash},
		},
		"[https://example.org/v A Song] sung by [[Ash Placeholder]], [[Rowan Placeholder]] & [[Wren Placeholder]]": {
			Performers: []string{ash, rowan, wren},
		},
		"[https://example.org/v A Song sung by Ash on his lute]": {},
	}
	for item, want := range tests {
		assert.Equal(t, want, summarise(item), item)
	}
}

func TestParsePerformanceText_YearBounds(t *testing.T) {
	for item, want := range map[string]int{
		"[https://example.org/2019/v A Song] in 1965":           0,
		"[https://example.org/v A Song] in 1966":                1966,
		"[https://example.org/v A Song] in 2027":                0,
		"[https://example.org/v A Song] in 2026, again in 2020": 2026,
		"[https://example.org/v A Song] at Event 45 in 12345":   0,
	} {
		assert.Equal(t, want, ParsePerformanceText(item, testMaxYear).Year, item)
	}
}

func TestParsePerformanceText_NoEvent(t *testing.T) {
	p := ParsePerformanceText("[https://example.org/at/v A Song] of [[Ash Placeholder]], 2020", testMaxYear)
	assert.Equal(t, TextSpan{}, p.Event)
	assert.Equal(t, TextSpan{}, ParsePerformanceText("at 2020", testMaxYear).Event)
	assert.Equal(t, TextSpan{}, p.Uploader)
	assert.Empty(t, p.Platform)
}

func TestParsePerformanceText_EventSpan(t *testing.T) {
	item := "[https://example.org/v A Song] at 2009 Spring Revel - a video"
	p := ParsePerformanceText(item, testMaxYear)
	assert.Equal(t, "Spring Revel", p.Event.Text)
	assert.Equal(t, p.Event.Text, item[p.Event.Start:p.Event.End])
	assert.Equal(t, 2009, p.Year)
}

func TestParsePerformanceText_EventCapitalisedAndCut(t *testing.T) {
	for item, want := range map[string]string{
		"[https://example.org/v A Song] at around the 10:52 mark":                   "",
		"[https://example.org/v A Song], beginning at 40:51":                        "",
		"[https://example.org/v A Song] at an [[:Category:Amtgard|Amtgard]] event":  "",
		"[https://example.org/v A Song, recorded at the Green Dragon for YouTube]":  greenDragon,
		"[https://example.org/v A Song] at the Green Dragon at Spring War on May 1": greenDragon,
		"[https://example.org/v A Song] at Coronation of Wren & Ash":                "Coronation of Wren & Ash",
	} {
		assert.Equal(t, want, ParsePerformanceText(item, testMaxYear).Event.Text, item)
	}
}

func TestParsePerformance_FullGrammar(t *testing.T) {
	rec := ParsePerformance(ListItem{Depth: 1, Raw: "[https://example.org/v Recording by Wren Placeholder on " +
		"YouTube] of [[Ash Placeholder]] and [[Rowan Placeholder]] singing [[A Song]] at Pennsic War, 2019"},
		testMaxYear, noWorks)

	assert.Equal(t, PerformanceRecord{
		DescriptionVerbatim: "[https://example.org/v Recording by Wren Placeholder on YouTube] of [[Ash Placeholder]] " +
			"and [[Rowan Placeholder]] singing [[A Song]] at Pennsic War, 2019",
		Uploader: wren, EventText: pennsicWar, Year: 2019, Performers: []string{ash, rowan}, Piece: aSong,
	}, rec)
}

func TestParsePerformance_VerbatimUnchanged(t *testing.T) {
	item := "[https://example.org/v ''A Song'']  sung   &amp; ''played'' by [[Ash Placeholder|Ash]] — 1999 "
	parsed := ParsePage("== Performances & Recordings ==\n** " + item + "\n")

	rec := ParsePerformance(parsed.Sections[1].Content.Items[0], testMaxYear, noWorks)

	assert.Equal(t, item[:len(item)-1], rec.DescriptionVerbatim, "only the list markers and trailing space go")
	assert.Equal(t, 1999, rec.Year)
}

func TestParsePerformance_YearBounds(t *testing.T) {
	year := func(raw string) int { return ParsePerformance(ListItem{Raw: raw}, testMaxYear, noWorks).Year }

	assert.Zero(t, year("[https://example.org/v A] in 1965"))
	assert.Zero(t, year("[https://example.org/v A] in 2027"))
	assert.Equal(t, 2026, year("[https://example.org/v A] in 2026"))
}

func TestParsePerformance_NoEvent(t *testing.T) {
	rec := ParsePerformance(ListItem{Raw: "[https://example.org/v A Song] of [[Ash Placeholder]]"}, testMaxYear,
		noWorks)

	assert.Empty(t, rec.EventText)
	assert.Empty(t, rec.Uploader)
	assert.Equal(t, []string{ash}, rec.Performers)
}

// noWorks is the isWork of a wiki without works.
func noWorks(string) bool { return false }

// TestParsePerformance_WorkLinkIsThePieceNotAPerformer: a link to a work is never a performer, after a comma or
// "of" included; with no "singing [[P]]", the first link to a work is the piece.
func TestParsePerformance_WorkLinkIsThePieceNotAPerformer(t *testing.T) {
	isWork := func(target string) bool { return target == aSong || target == twoSongs }
	for raw, want := range map[string]PerformanceRecord{
		"[https://example.org/v Video] performing his original piece, [[A Song]]":    {Piece: aSong},
		"[https://example.org/v Video] of his original poem, [[Two Songs]]":          {Piece: twoSongs},
		"[https://example.org/v Video] performing [[A Song]] by [[Ash Placeholder]]": {Piece: aSong, Performers: []string{ash}},
		"[https://example.org/v Video] of [[Ash Placeholder]] singing [[Two Songs]], [[A Song]]": {Piece: twoSongs,
			Performers: []string{ash}},
		"[https://example.org/v Video] her song [[A Song]] at Pennsic War": {Piece: aSong, EventText: pennsicWar},
	} {
		rec := ParsePerformance(ListItem{Raw: raw}, testMaxYear, isWork)
		want.DescriptionVerbatim = raw
		assert.Equal(t, want, rec, raw)
	}
}

func TestLatestYear(t *testing.T) {
	assert.Equal(t, MinPerformanceYear, LatestYear(nil))
	assert.Equal(t, 2024, LatestYear([]Page{
		{LastRevTimestamp: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)},
		{LastRevTimestamp: time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
	}))
}
