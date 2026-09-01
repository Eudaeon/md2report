package markdown

import "testing"

func TestParseInlines(t *testing.T) {
	inls := parseInlines("some **bold**, some *italic*, some `code` and a [link](https://example.org).")
	find := func(text string) Inline {
		for _, in := range inls {
			if in.Text == text {
				return in
			}
		}
		t.Fatalf("run %q missing from %+v", text, inls)
		return Inline{}
	}
	if !find("bold").Bold {
		t.Error("**bold** not detected")
	}
	if !find("italic").Italic {
		t.Error("*italic* not detected")
	}
	if !find("code").Code {
		t.Error("`code` not detected")
	}
	if find("link").Href != "https://example.org" {
		t.Error("link not detected")
	}
	if in := parseInlines("a_variable_name"); len(in) != 1 || in[0].Italic {
		t.Errorf("word-internal underscores must not open italics: %+v", in)
	}
}

// A destination may hold parentheses of its own, as Wikipedia URLs do.
func TestLinkDestinationWithParentheses(t *testing.T) {
	inls := parseInlines("see [x](https://host/a_(b)) now")
	var href string
	for _, in := range inls {
		if in.Text == "x" {
			href = in.Href
		}
	}
	if href != "https://host/a_(b)" {
		t.Errorf("href = %q, expected the whole destination", href)
	}
	if got := Plain(inls); got != "see x now" {
		t.Errorf("text = %q: no stray parenthesis should be left behind", got)
	}
}
