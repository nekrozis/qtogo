package repository

import (
	"fmt"
	"net/url"
	"strings"
)

// IndexEntry is one link on a directory listing.
//
// Name comes from the link target, not from the text: the page shows "Details"
// for a mirrorlist companion, and a name taken from the text would be both
// useless and ambiguous.
type IndexEntry struct {
	Name  string // the child's name, taken from the link target
	Text  string // the text the page shows for it
	URL   string // absolute, resolved against the page URL
	IsDir bool
}

// ParseIndex reads an Apache-style directory listing.
//
// The listing is the only authority on what a repository contains, so every link
// is scanned rather than a column position trusted, and a page that is not a
// listing is refused outright: a mirror or error page looks exactly like an empty
// directory to a naive parser, which would turn "the version is not there" into
// "there is nothing to download".
//
// Entries come back as the page presents them, including file companions such as
// `.mirrorlist`; filtering by name is the caller's business.
func ParseIndex(pageURL string, body []byte) ([]IndexEntry, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, fmt.Errorf("listing URL %q: %w", pageURL, err)
	}

	got := listingPath(body)
	if got == "" {
		return nil, fmt.Errorf("%s is not a directory listing", pageURL)
	}
	if want := strings.TrimRight(base.EscapedPath(), "/"); want != "" && strings.TrimRight(got, "/") != want {
		return nil, fmt.Errorf("%s describes a different path (%s)", pageURL, got)
	}

	entries := make([]IndexEntry, 0, 16)
	for _, a := range scanAnchors(body) {
		if entry, ok := toEntry(base, a); ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

// listingPath returns the path a listing page claims to show, or "" when the page
// does not look like a listing at all.
func listingPath(body []byte) string {
	const marker = "Index of "

	text := string(body)
	i := strings.Index(text, marker)
	if i < 0 {
		return ""
	}
	rest := text[i+len(marker):]
	if j := strings.IndexAny(rest, "<\r\n"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}

// toEntry applies the rules that decide whether a link names a child of this
// directory, and resolves the ones that do.
func toEntry(base *url.URL, a anchor) (IndexEntry, bool) {
	href := strings.TrimSpace(a.href)
	if href == "" {
		return IndexEntry{}, false // the sortable column headers
	}
	ref, err := url.Parse(href)
	if err != nil || ref.Scheme != "" || ref.Host != "" {
		return IndexEntry{}, false // the mirror and Apache links in the footer
	}
	if strings.HasPrefix(href, "/") || strings.HasPrefix(href, "?") {
		return IndexEntry{}, false // "Parent Directory" and the sort links
	}

	path := strings.TrimPrefix(ref.Path, "./")
	path = strings.Trim(path, "/")
	if path == "" || path == "." || path == ".." {
		return IndexEntry{}, false
	}
	if strings.Contains(path, "/") {
		return IndexEntry{}, false // something below this directory, not a child
	}

	// Either the link or its text carries the trailing slash that marks a
	// directory; the listing uses both.
	text := cleanText(a.text)
	isDir := strings.HasSuffix(href, "/") || strings.HasSuffix(strings.TrimSpace(a.text), "/")

	return IndexEntry{
		Name:  path,
		Text:  text,
		URL:   base.ResolveReference(ref).String(),
		IsDir: isDir,
	}, true
}

// anchor is one <a> element: where it points and the text it shows.
type anchor struct{ href, text string }

// scanAnchors finds every <a> element in a listing.
//
// This is not a general HTML parser and does not try to be: a directory listing
// is a narrow, machine-generated shape, and the variations it takes are the ones
// the fixtures cover.
func scanAnchors(body []byte) []anchor {
	s := string(body)
	anchors := make([]anchor, 0, 16)

	for i := 0; ; {
		open := indexFold(s, i, "<a")
		if open < 0 {
			return anchors
		}
		if open+2 < len(s) && !isNameBoundary(s[open+2]) {
			i = open + 2
			continue // <address>, <abbr> and other tags that start the same way
		}

		tagEnd := indexByteOutsideQuotes(s, open, '>')
		if tagEnd < 0 {
			return anchors
		}
		text, next := elementText(s, tagEnd+1)
		if href, ok := attribute(s[open:tagEnd], "href"); ok {
			anchors = append(anchors, anchor{href: href, text: text})
		}
		i = next
	}
}

// elementText returns an element's text with any nested markup dropped, and the
// offset just past the closing tag that ends it.
func elementText(s string, from int) (string, int) {
	var text strings.Builder

	for i := from; i < len(s); {
		if s[i] != '<' {
			text.WriteByte(s[i])
			i++
			continue
		}
		end := indexByteOutsideQuotes(s, i, '>')
		if end < 0 {
			break // unterminated markup: take what there is
		}
		if isClosingTag(s[i : end+1]) {
			return cleanText(text.String()), end + 1
		}
		i = end + 1
	}
	return cleanText(text.String()), len(s)
}

// isClosingTag reports whether a tag is a closing tag, such as "</a>".
func isClosingTag(tag string) bool {
	return strings.HasPrefix(strings.TrimLeft(tag[1:], " \t\r\n"), "/")
}

// attribute returns an attribute's value from a start tag, accepting double
// quotes, single quotes and an unquoted value.
func attribute(tag, name string) (string, bool) {
	for i := 0; ; {
		at := indexFold(tag, i, name)
		if at < 0 {
			return "", false
		}
		// The name has to stand on its own, not be the tail of a longer
		// attribute name or the inside of another value.
		if at > 0 && !isNameBoundary(tag[at-1]) {
			i = at + len(name)
			continue
		}

		rest := strings.TrimLeft(tag[at+len(name):], " \t\r\n")
		if !strings.HasPrefix(rest, "=") {
			i = at + len(name)
			continue
		}
		rest = strings.TrimLeft(rest[1:], " \t\r\n")
		if rest == "" {
			return "", false
		}

		if quote := rest[0]; quote == '"' || quote == '\'' {
			// The tag slice ends before its closing angle bracket, and a tag
			// whose quote is left open never produces one, so the closing quote
			// is always here.
			end := strings.IndexByte(rest[1:], quote)
			return rest[1 : 1+end], true
		}
		end := strings.IndexAny(rest, " \t\r\n>")
		if end < 0 {
			end = len(rest)
		}
		return rest[:end], true
	}
}

// isNameBoundary reports whether b ends a tag or attribute name.
func isNameBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '>' || b == '/' || b == '='
}

// indexFold is strings.Index with ASCII case folding, searching from an offset.
func indexFold(s string, from int, sub string) int {
	if from >= len(s) {
		return -1
	}
	at := strings.Index(strings.ToLower(s[from:]), strings.ToLower(sub))
	if at < 0 {
		return -1
	}
	return from + at
}

// indexByteOutsideQuotes finds the next b that is not inside a quoted value.
func indexByteOutsideQuotes(s string, from int, b byte) int {
	var quote byte
	for i := from; i < len(s); i++ {
		switch {
		case quote != 0:
			if s[i] == quote {
				quote = 0
			}
		case s[i] == '"' || s[i] == '\'':
			quote = s[i]
		case s[i] == b:
			return i
		}
	}
	return -1
}

// cleanText collapses a link's text to single spaces.
func cleanText(s string) string { return strings.Join(strings.Fields(s), " ") }
