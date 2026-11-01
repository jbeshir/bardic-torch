package domain

import (
	"errors"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// URLs several cases normalize to.
const (
	exampleA       = "https://example.org/a"
	exampleRoot    = "https://example.org/"
	httpExampleA   = "http://example.org/a"
	canonicalVideo = "https://youtube.com/watch?v=dQw4w9WgXcQ"
	punycodeRoot   = "https://xn--bcher-kva.example/"
)

// urlCase is a URL and its normalized form.
type urlCase struct {
	name, raw, want string
}

var (
	trackingParameterCases = []urlCase{
		{"utm_source", "https://example.org/a?utm_source=x", exampleA},
		{"utm_medium", "https://example.org/a?b=1&utm_medium=social", "https://example.org/a?b=1"},
		{"utm_campaign_upper_case", "https://example.org/a?UTM_Campaign=x&b=1", "https://example.org/a?b=1"},
		{"utm_escaped_underscore", "https://example.org/a?utm%5fterm=x", exampleA},
		{"fbclid", "https://example.org/a?fbclid=IwAR0", exampleA},
		{"gclid", "https://example.org/a?gclid=x", exampleA},
		{"igshid", "https://instagram.com/p/x/?igshid=abc", "https://instagram.com/p/x/"},
		{"mc_cid", "https://example.org/a?mc_cid=1", exampleA},
		{"mc_eid", "https://example.org/a?mc_eid=1", exampleA},
		{"feature", "https://youtube.com/channel/UCx?feature=share", "https://youtube.com/channel/UCx"},
		{"pp", "https://youtube.com/results?pp=ygU&search_query=bard",
			"https://youtube.com/results?search_query=bard"},
		{"ab_channel", "https://youtube.com/@bard/videos?ab_channel=Bard", "https://youtube.com/@bard/videos"},
		{"authuser", "https://drive.google.com/file/d/x/view?authuser=0", "https://drive.google.com/file/d/x/view"},
		{"start_radio", "https://youtube.com/playlist?list=PLabc&start_radio=1",
			"https://youtube.com/playlist?list=PLabc"},
		{"si_youtube", "https://youtube.com/playlist?list=PLabc&si=tok", "https://youtube.com/playlist?list=PLabc"},
		{"si_spotify", "https://open.spotify.com/track/4x?si=abc", "https://open.spotify.com/track/4x"},
		{"youtube_mix_with_index", "https://youtube.com/playlist?index=3&list=RDabc&b=1",
			"https://youtube.com/playlist?b=1"},
		{"only_tracking_leaves_no_question_mark", "https://example.org/?utm_source=x&fbclid=y",
			exampleRoot},
	}

	meaningfulParameterCases = []urlCase{
		{"si_elsewhere_is_kept", "https://example.org/a?si=2", "https://example.org/a?si=2"},
		{"youtube_playlist_list", "https://youtube.com/playlist?list=PLx1", "https://youtube.com/playlist?list=PLx1"},
		{"index_kept_without_mix", "https://example.org/a?list=PL1&index=2", "https://example.org/a?index=2&list=PL1"},
		{"wiki_title_action", "https://example.org/index.php?title=Kettle&action=edit",
			"https://example.org/index.php?action=edit&title=Kettle"},
		{"catid_id", "https://example.org/x?id=7&catid=3", "https://example.org/x?catid=3&id=7"},
		{"research_keys", "https://example.org/b?topic=9&brd_nm=W&brd_id=2&bp_id=4&bandID=1&atlantian_id=5",
			"https://example.org/b?atlantian_id=5&bandID=1&bp_id=4&brd_id=2&brd_nm=W&topic=9"},
		{"sort_is_stable", "https://example.org/a?b=2&a=1&b=1", "https://example.org/a?a=1&b=2&b=1"},
		{"plus_is_kept", "https://example.org/s?q=a+b", "https://example.org/s?q=a+b"},
		{"key_without_value_differs_from_empty_value", "https://example.org/s?a&b=", "https://example.org/s?a&b="},
		{"empty_parameters_dropped", "https://example.org/s?&a=1&&", "https://example.org/s?a=1"},
		{"bare_question_mark", "https://example.org/s?", "https://example.org/s"},
		{"escaped_ampersand_stays_in_value", "https://example.org/s?q=a%26b", "https://example.org/s?q=a%26b"},
		{"value_holds_equals", "https://example.org/s?q=a=b", "https://example.org/s?q=a=b"},
		{"fragment_locator", "https://example.org/book.pdf#page=63", "https://example.org/book.pdf#page=63"},
		{"empty_fragment_dropped", "https://example.org/a#", exampleA},
	}

	youTubeCases = []urlCase{
		{"youtu_be", "https://youtu.be/dQw4w9WgXcQ", canonicalVideo},
		{"youtu_be_http_share", "http://youtu.be/dQw4w9WgXcQ?si=tok", canonicalVideo},
		{"watch", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", canonicalVideo},
		{"watch_http", "http://youtube.com/watch?v=dQw4w9WgXcQ", canonicalVideo},
		{"mobile", "https://m.youtube.com/watch?v=dQw4w9WgXcQ&feature=youtu.be",
			canonicalVideo},
		{"shorts", "https://youtube.com/shorts/dQw4w9WgXcQ?feature=share", canonicalVideo},
		{"embed", "https://www.youtube.com/embed/dQw4w9WgXcQ", canonicalVideo},
		{"keeps_time_and_playlist", "https://youtu.be/dQw4w9WgXcQ?t=42&list=PLx&index=2&ab_channel=B",
			"https://youtube.com/watch?index=2&list=PLx&t=42&v=dQw4w9WgXcQ"},
		{"drops_mix", "https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDdQw4w9WgXcQ&index=1&start_radio=1",
			canonicalVideo},
		{"drops_other_parameters", "https://youtube.com/watch?v=dQw4w9WgXcQ&app=desktop",
			canonicalVideo},
		{"channel_keeps_scheme", "http://www.youtube.com/user/bard", "http://youtube.com/user/bard"},
		{"invalid_id_is_not_canonical", "https://youtu.be/a%20b", "https://youtu.be/a%20b"},
	}

	hostAndPortCases = []urlCase{
		{"scheme_and_host_case", "HTTPS://Example.ORG/Path", "https://example.org/Path"},
		{"idn_to_punycode", "https://bücher.example/", punycodeRoot},
		{"punycode_kept", punycodeRoot, punycodeRoot},
		{"trailing_dot", "https://example.org./a", exampleA},
		{"default_https_port", "https://example.org:443/a", exampleA},
		{"default_http_port", "http://example.org:80/a", httpExampleA},
		{"other_port_kept", "http://example.org:8080/a", "http://example.org:8080/a"},
		{"port_leading_zeros", "https://example.org:0443/a", exampleA},
		{"empty_port", "https://example.org:/a", exampleA},
		{"www_dropped", "https://www.example.org/a", exampleA},
		{"repeated_www_dropped", "https://www.www.example.org/a", exampleA},
		{"www_kept_when_it_leaves_no_dot", "https://www.com/a", "https://www.com/a"},
		{"http_kept", httpExampleA, httpExampleA},
		{"underscore_in_label", "https://my_site.example.org/", "https://my_site.example.org/"},
		{"ipv4", "http://192.0.2.1/x", "http://192.0.2.1/x"},
		{"ipv6", "http://[2001:DB8:0:0::1]:8080/x", "http://[2001:db8::1]:8080/x"},
	}

	pathEncodingCases = []urlCase{
		{"unreserved_decoded", "https://example.org/%7euser/%41", "https://example.org/~user/A"},
		{"reserved_upper_cased", "https://example.org/a%2fb", "https://example.org/a%2Fb"},
		{"non_ascii_encoded", "https://example.org/café", "https://example.org/caf%C3%A9"},
		{"space_encoded", "https://example.org/a b", "https://example.org/a%20b"},
		{"stray_percent", "https://example.org/100%", "https://example.org/100%25"},
		{"dot_segments", "https://example.org/a/./b/../c", "https://example.org/a/c"},
		{"encoded_dot_segments", "https://example.org/a/%2e%2E/c", "https://example.org/c"},
		{"dot_dot_above_root", "https://example.org/../a", exampleA},
		{"trailing_dot_segment", "https://example.org/a/b/..", "https://example.org/a/"},
		{"empty_path", "https://example.org", exampleRoot},
		{"empty_path_with_query", "https://example.org?a=1", "https://example.org/?a=1"},
		{"trailing_slash_and_case_kept", "https://example.org/Songs/", "https://example.org/Songs/"},
		{"sub_delimiters_kept", "https://example.org/a;b=c,d+e:f@g", "https://example.org/a;b=c,d+e:f@g"},
		{"disallowed_ascii_encoded", "https://example.org/a\"b<c>|^`{}\\", "https://example.org/a%22b%3Cc%3E%7C%5E%60%7B%7D%5C"},
		{"fragment_normalized", "https://example.org/a#Sec%74ion é", "https://example.org/a#Section%20%C3%A9"},
		{"fragment_hash_encoded", "https://example.org/a#b#c", "https://example.org/a#b%23c"},
	}

	schemeRelativeCases = []urlCase{
		{"scheme_relative", "//example.org/a", exampleA},
		{"bare_www", "www.example.org/a", exampleA},
		{"bare_www_upper_case", "WWW.Example.org", exampleRoot},
		{"surrounding_space", " \thttps://example.org/a \n", exampleA},
	}
)

func runURLCases(t *testing.T, cases []urlCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeURL(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			again, err := NormalizeURL(got)
			require.NoError(t, err)
			assert.Equal(t, got, again, "idempotent")
		})
	}
}

func TestNormalizeURL_StripsTrackingParameters(t *testing.T) {
	t.Parallel()
	runURLCases(t, trackingParameterCases)
}

func TestNormalizeURL_KeepsMeaningfulParameters(t *testing.T) {
	t.Parallel()
	runURLCases(t, meaningfulParameterCases)
}

func TestNormalizeURL_YouTubeForms(t *testing.T) {
	t.Parallel()
	runURLCases(t, youTubeCases)
}

func TestNormalizeURL_HostAndPort(t *testing.T) {
	t.Parallel()
	runURLCases(t, hostAndPortCases)
}

func TestNormalizeURL_PathEncoding(t *testing.T) {
	t.Parallel()
	runURLCases(t, pathEncodingCases)
}

func TestNormalizeURL_SchemeRelativeAndBareWWW(t *testing.T) {
	t.Parallel()
	runURLCases(t, schemeRelativeCases)
}

var rejectedURLs = map[string]string{
	"mailto":              "mailto:wren@example.invalid",
	"javascript":          "javascript:alert(1)",
	"ftp":                 "ftp://example.org/file",
	"empty_string":        "",
	"spaces_only":         "   ",
	"no_scheme":           "example.org/a",
	"no_slashes":          "https:example.org",
	"no_host":             "https:///a",
	"no_host_with_port":   "https://:80/a",
	"user_information":    "https://user@example.org/",
	"bad_port":            "https://example.org:http/",
	"port_out_of_range":   "https://example.org:65536/",
	"invalid_host_char":   "https://exa!mple.org/",
	"empty_label":         "https://example..org/",
	"percent_in_host":     "https://exa%41mple.org/",
	"label_too_long":      "https://" + strings.Repeat("a", 64) + ".org/",
	"unclosed_ipv6":       "http://[2001:db8::1/x",
	"ipv4_in_brackets":    "http://[192.0.2.1]/x",
	"ipv6_zone":           "http://[fe80::1%25eth0]/x",
	"invalid_utf8_host":   "https://ex\xffample.org/",
	"over_2048_bytes":     exampleRoot + strings.Repeat("a", MaxURLLength),
	"grows_over_2048":     exampleRoot + strings.Repeat("é", 400),
	"scheme_with_garbage": "ht tp://example.org/",
}

func TestNormalizeURL_Rejects(t *testing.T) {
	t.Parallel()

	for name, raw := range rejectedURLs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizeURL(raw)
			require.ErrorIs(t, err, ErrInvalidURL)
			assert.Empty(t, got)
		})
	}
}

// TestNormalizeURL_ErrorsHoldNoURL checks that error messages never repeat the URL, which may hold anything.
func TestNormalizeURL_ErrorsHoldNoURL(t *testing.T) {
	t.Parallel()

	_, err := NormalizeURL("https://leakmark@example.org/")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "leakmark")
}

func FuzzNormalizeURL(f *testing.F) {
	for _, cases := range [][]urlCase{
		trackingParameterCases, meaningfulParameterCases, youTubeCases, hostAndPortCases, pathEncodingCases,
		schemeRelativeCases,
	} {
		for _, tc := range cases {
			f.Add(tc.raw)
		}
	}
	for _, raw := range rejectedURLs {
		f.Add(raw)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		got, err := NormalizeURL(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("error %v is not ErrInvalidURL", err)
			}
			return
		}
		requireNormalizedForm(t, got)
		again, err := NormalizeURL(got)
		if err != nil || again != got {
			t.Fatalf("NormalizeURL(%q) = %q, %v; not idempotent", got, again, err)
		}
		if repeat, _ := NormalizeURL(raw); repeat != got {
			t.Fatalf("NormalizeURL(%q) gave %q, then %q", raw, got, repeat)
		}
	})
}

// requireNormalizedForm fails t unless got is ASCII, at most MaxURLLength bytes and accepted by url.Parse.
func requireNormalizedForm(t *testing.T, got string) {
	t.Helper()

	for i := range len(got) {
		if got[i] >= utf8.RuneSelf {
			t.Fatalf("%q is not ASCII", got)
		}
	}
	if len(got) > MaxURLLength {
		t.Fatalf("%q is longer than %d bytes", got, MaxURLLength)
	}
	if _, err := url.Parse(got); err != nil {
		t.Fatalf("url.Parse(%q): %v", got, err)
	}
}
