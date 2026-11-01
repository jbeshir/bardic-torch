package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/net/idna"
)

// MaxURLLength is the longest URL a link holds, in bytes: the width of the links URL columns.
const MaxURLLength = 2048

// ErrInvalidURL is returned by NormalizeURL for a string that is not an http or https URL with a host, or whose
// normalized form would be longer than MaxURLLength. Its messages never hold the URL.
var ErrInvalidURL = errors.New("invalid URL")

// The schemes and YouTube hosts NormalizeURL names.
const (
	schemeHTTP        = "http"
	schemeHTTPS       = "https"
	youTubeHost       = "youtube.com"
	mobileYouTubeHost = "m.youtube.com"
)

// NormalizeURL returns the normalized form of an http or https URL: the key that identifies a link
// (links.normalized_url), so that spellings of one address that differ only in ways no server distinguishes, or
// in tracking parameters, are one link. It is deterministic and idempotent, and its result is ASCII, at most
// MaxURLLength bytes, and accepted by net/url. The rules, in order:
//
//   - ASCII space around the URL is trimmed. A scheme-relative URL ("//host/…") and one starting "www." with no
//     scheme get https. Anything but http and https, a URL without a host, and one with user information fail with
//     ErrInvalidURL.
//   - The scheme is lower-cased and never upgraded from http to https, except in the YouTube form below. The host
//     is lower-cased, converted to IDNA ASCII (punycode) and loses a trailing dot, a default port (:80 for http,
//     :443 for https) and each leading "www." label that leaves a host with a dot. A host label may hold ASCII
//     letters, digits, hyphens and underscores (as some real subdomains do), and nothing else. IPv6 literals are
//     written in their canonical form.
//   - A YouTube video, as youtu.be/ID, (m.)youtube.com/watch?v=ID, /shorts/ID or /embed/ID, becomes
//     https://youtube.com/watch?v=ID, keeping only its t, list and index parameters. This is the one scheme
//     upgrade: every spelling of a video is the same resource, and YouTube serves only https.
//   - In the path, query and fragment, percent-encoded unreserved characters are decoded, other percent escapes
//     upper-cased, a "%" that starts no escape encoded as %25, and non-ASCII bytes, controls, spaces and the
//     characters RFC 3986 does not allow there percent-encoded. Dot segments are removed from the path, an empty
//     path becomes "/", and the path keeps its case and any trailing slash.
//   - The query is split by hand, so "+" stays "+" and "?a" stays distinct from "?a=". Tracking parameters are
//     dropped (see isTrackingParameter), as is a YouTube mix (list=RD…) with its index. The other parameters are
//     sorted stably by key; an empty query has no "?".
//   - A non-empty fragment is kept (it may be a locator, as in "#page=63").
func NormalizeURL(raw string) (string, error) {
	s := strings.Trim(raw, " \t\n\v\f\r")
	switch {
	case strings.HasPrefix(s, "//"):
		s = "https:" + s
	case len(s) >= 4 && strings.EqualFold(s[:4], "www."):
		s = "https://" + s
	}

	parts, err := splitURL(s)
	if err != nil {
		return "", err
	}
	host, err := normalizeHost(parts.host)
	if err != nil {
		return "", err
	}
	port, err := normalizePort(parts.scheme, parts.port)
	if err != nil {
		return "", err
	}
	u := normalizedURL{
		scheme:   parts.scheme,
		host:     host + port,
		path:     removeDotSegments(percentNormalize(parts.path, pathAllowed)),
		params:   normalizeQuery(host, parts.query),
		fragment: percentNormalize(parts.fragment, fragmentAllowed),
	}
	if u.path == "" {
		u.path = "/"
	}
	if video, ok := youTubeVideo(host, port, u); ok {
		u = video
	}

	normalized := u.String()
	if len(normalized) > MaxURLLength {
		return "", fmt.Errorf("%w: longer than %d bytes", ErrInvalidURL, MaxURLLength)
	}

	return normalized, nil
}

// urlParts are the raw components of a URL, split but not yet normalized.
type urlParts struct {
	scheme, host, port, path, query, fragment string
}

// splitURL splits an absolute http or https URL with an authority into its components.
func splitURL(s string) (urlParts, error) {
	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return urlParts{}, fmt.Errorf("%w: no scheme", ErrInvalidURL)
	}
	p := urlParts{scheme: strings.ToLower(s[:colon])}
	if p.scheme != schemeHTTP && p.scheme != schemeHTTPS {
		return urlParts{}, fmt.Errorf("%w: scheme is not http or https", ErrInvalidURL)
	}
	rest, ok := strings.CutPrefix(s[colon+1:], "//")
	if !ok {
		return urlParts{}, fmt.Errorf("%w: no host", ErrInvalidURL)
	}

	rest, p.fragment, _ = strings.Cut(rest, "#")
	rest, p.query, _ = strings.Cut(rest, "?")
	authority := rest
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		authority, p.path = rest[:slash], rest[slash:]
	}
	if strings.Contains(authority, "@") {
		return urlParts{}, fmt.Errorf("%w: user information", ErrInvalidURL)
	}

	p.host = authority
	if strings.HasPrefix(authority, "[") {
		end := strings.IndexByte(authority, ']')
		if end < 0 {
			return urlParts{}, fmt.Errorf("%w: unclosed IPv6 literal", ErrInvalidURL)
		}
		p.host, p.port = authority[:end+1], authority[end+1:]
	} else if i := strings.LastIndexByte(authority, ':'); i >= 0 {
		p.host, p.port = authority[:i], authority[i:]
	}
	if p.host == "" {
		return urlParts{}, fmt.Errorf("%w: no host", ErrInvalidURL)
	}

	return p, nil
}

// hostProfile maps hosts for lookup as browsers do: UTS #46 mapping and validation with the Bidi rule, but
// allowing labels such as "r3---cdn" and ASCII outside RFC 1034's letters, digits and hyphen, which isHostLabel
// then limits to underscores.
var hostProfile = idna.New(idna.MapForLookup(), idna.BidiRule(), idna.StrictDomainName(false),
	idna.CheckHyphens(false))

// normalizeHost returns the normalized form of a URL's host: lower-cased IDNA ASCII without a trailing dot or
// leading "www." labels, or a canonical IPv6 literal.
func normalizeHost(host string) (string, error) {
	if literal, ok := strings.CutPrefix(host, "["); ok {
		addr, err := netip.ParseAddr(strings.TrimSuffix(literal, "]"))
		if err != nil || !addr.Is6() || addr.Zone() != "" {
			return "", fmt.Errorf("%w: invalid IPv6 literal", ErrInvalidURL)
		}
		return "[" + addr.String() + "]", nil
	}

	ascii, err := hostProfile.ToASCII(strings.TrimSuffix(host, "."))
	if err != nil {
		return "", fmt.Errorf("%w: invalid host", ErrInvalidURL)
	}
	ascii = strings.ToLower(ascii)
	labels := strings.Split(ascii, ".")
	if len(ascii) > 253 || !allHostLabels(labels) {
		return "", fmt.Errorf("%w: invalid host", ErrInvalidURL)
	}
	for len(labels) > 2 && labels[0] == "www" {
		labels = labels[1:]
	}

	return strings.Join(labels, "."), nil
}

func allHostLabels(labels []string) bool {
	for _, label := range labels {
		if !isHostLabel(label) {
			return false
		}
	}
	return true
}

// isHostLabel reports whether label is a host label NormalizeURL accepts: 1 to 63 lower-case ASCII letters,
// digits, hyphens and underscores.
func isHostLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	for i := range len(label) {
		c := label[i]
		if !isLowerAlnum(c) && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

func isLowerAlnum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// normalizePort returns the port part of a normalized URL (":" and the port without leading zeros), or "" for no
// port or the scheme's default one.
func normalizePort(scheme, port string) (string, error) {
	if port == "" || port == ":" {
		return "", nil
	}
	digits, ok := strings.CutPrefix(port, ":")
	n, err := strconv.ParseUint(digits, 10, 16)
	if !ok || err != nil {
		return "", fmt.Errorf("%w: invalid port", ErrInvalidURL)
	}
	if (scheme == schemeHTTP && n == 80) || (scheme == schemeHTTPS && n == 443) {
		return "", nil
	}

	return ":" + strconv.FormatUint(n, 10), nil
}

// urlParam is one parameter of a query, normalized: a key without "=" ("?a") is distinct from one with an empty
// value ("?a=").
type urlParam struct {
	key, value string
	hasValue   bool
}

func (p urlParam) String() string {
	if !p.hasValue {
		return p.key
	}
	return p.key + "=" + p.value
}

// normalizeQuery returns the parameters of a raw query that NormalizeURL keeps, normalized and sorted stably by
// key. Empty parameters ("a&&b") are dropped.
func normalizeQuery(host, query string) []urlParam {
	var params []urlParam
	mix := false
	for field := range strings.SplitSeq(query, "&") {
		if field == "" {
			continue
		}
		key, value, hasValue := strings.Cut(field, "=")
		p := urlParam{
			key:      percentNormalize(key, queryKeyAllowed),
			value:    percentNormalize(value, queryValueAllowed),
			hasValue: hasValue,
		}
		if p.key == "list" && strings.HasPrefix(p.value, "RD") {
			mix = true
			continue
		}
		if !isTrackingParameter(host, p.key) {
			params = append(params, p)
		}
	}
	if mix {
		params = slices.DeleteFunc(params, func(p urlParam) bool { return p.key == "index" })
	}
	slices.SortStableFunc(params, func(a, b urlParam) int { return strings.Compare(a.key, b.key) })

	return params
}

// trackingParameters are the query parameters that say how a visitor came to a page, not which page it is,
// matched case-insensitively. utm_* parameters are tracking parameters too.
var trackingParameters = []string{
	"fbclid", "gclid", "igshid", "mc_cid", "mc_eid", "feature", "pp", "ab_channel", "authuser", "start_radio",
}

// shareHosts are the hosts whose si parameter is a share-tracking token; elsewhere si may mean anything.
var shareHosts = []string{youTubeHost, mobileYouTubeHost, "music.youtube.com", "youtu.be", "open.spotify.com"}

// isTrackingParameter reports whether NormalizeURL drops the parameter key of a URL of the normalized host.
func isTrackingParameter(host, key string) bool {
	key = strings.ToLower(key)
	switch {
	case strings.HasPrefix(key, "utm_"), slices.Contains(trackingParameters, key):
		return true
	case key == "si":
		return slices.Contains(shareHosts, host)
	default:
		return false
	}
}

// normalizedURL is a URL whose components are normalized.
type normalizedURL struct {
	scheme, host, path string
	params             []urlParam
	fragment           string
}

func (u normalizedURL) String() string {
	var b strings.Builder
	b.WriteString(u.scheme)
	b.WriteString("://")
	b.WriteString(u.host)
	b.WriteString(u.path)
	for i, p := range u.params {
		if i == 0 {
			b.WriteByte('?')
		} else {
			b.WriteByte('&')
		}
		b.WriteString(p.String())
	}
	if u.fragment != "" {
		b.WriteByte('#')
		b.WriteString(u.fragment)
	}

	return b.String()
}

// youTubeVideoParameters are the parameters a YouTube video URL keeps besides v.
var youTubeVideoParameters = []string{"index", "list", "t"}

// youTubeVideo returns the canonical form of u, a normalized URL with the given host and port, if it is a
// YouTube video URL.
func youTubeVideo(host, port string, u normalizedURL) (normalizedURL, bool) {
	if port != "" {
		return normalizedURL{}, false
	}
	var id string
	switch host {
	case "youtu.be":
		id = strings.TrimPrefix(u.path, "/")
	case youTubeHost, mobileYouTubeHost:
		if u.path == "/watch" {
			for _, p := range u.params {
				if p.key == "v" {
					id = p.value
					break
				}
			}
		} else if rest, ok := strings.CutPrefix(u.path, "/shorts/"); ok {
			id = rest
		} else if rest, ok := strings.CutPrefix(u.path, "/embed/"); ok {
			id = rest
		}
	}
	if !isYouTubeID(id) {
		return normalizedURL{}, false
	}

	video := normalizedURL{scheme: schemeHTTPS, host: youTubeHost, path: "/watch", fragment: u.fragment}
	for _, p := range u.params {
		if slices.Contains(youTubeVideoParameters, p.key) {
			video.params = append(video.params, p)
		}
	}
	video.params = append(video.params, urlParam{key: "v", value: id, hasValue: true})
	slices.SortStableFunc(video.params, func(a, b urlParam) int { return strings.Compare(a.key, b.key) })

	return video, true
}

// isYouTubeID reports whether id has the form of a YouTube video ID: ASCII letters, digits, "-" and "_".
func isYouTubeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := range len(id) {
		c := id[i]
		if !isLowerAlnum(c) && (c < 'A' || c > 'Z') && c != '-' && c != '_' {
			return false
		}
	}
	return true
}

// The characters each URL component holds unencoded besides the unreserved ones (RFC 3986): its sub-delimiters
// and separators, less those that delimit the component itself.
const (
	pathAllowed       = "!$&'()*+,;=:@/"
	queryKeyAllowed   = "!$'()*+,;:@/?"
	queryValueAllowed = "!$'()*+,;=:@/?"
	fragmentAllowed   = "!$&'()*+,;=:@/?"
)

// percentNormalize returns s with percent-encoded unreserved characters decoded, other escapes upper-cased, and
// every byte that is neither unreserved nor in allowed percent-encoded, including a "%" that starts no escape.
func percentNormalize(s, allowed string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			decoded := unhex(s[i+1])<<4 | unhex(s[i+2])
			if isUnreserved(decoded) {
				b.WriteByte(decoded)
			} else {
				writeEscape(&b, decoded)
			}
			i += 2
			continue
		}
		if isUnreserved(c) || (c < 0x80 && c != '%' && strings.IndexByte(allowed, c) >= 0) {
			b.WriteByte(c)
		} else {
			writeEscape(&b, c)
		}
	}

	return b.String()
}

func isUnreserved(c byte) bool {
	return isLowerAlnum(c) || (c >= 'A' && c <= 'Z') || c == '-' || c == '.' || c == '_' || c == '~'
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	default:
		return c - '0'
	}
}

func writeEscape(b *strings.Builder, c byte) {
	const digits = "0123456789ABCDEF"
	b.WriteByte('%')
	b.WriteByte(digits[c>>4])
	b.WriteByte(digits[c&0xF])
}

// removeDotSegments removes the "." and ".." segments of a path (RFC 3986 section 5.2.4).
func removeDotSegments(path string) string {
	if path == "" {
		return ""
	}
	var out []string
	segments := strings.Split(path, "/")[1:]
	for i, segment := range segments {
		last := i == len(segments)-1
		switch segment {
		case ".":
			if last {
				out = append(out, "")
			}
		case "..":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, segment)
		}
	}

	return "/" + strings.Join(out, "/")
}
