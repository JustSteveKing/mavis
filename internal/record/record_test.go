package record

import (
	"strings"
	"testing"
)

func TestRoundTripKeepsWhatAHumanAdded(t *testing.T) {
	in := `---
type: client
status: active # moved after the September call
name: Acme Ltd
favourite_biscuit: hobnob
with: [Jo Bloggs, Sam]
created: 2026-10-01
---
# Acme

Body text, kept exactly.
  Including odd indentation.
`
	d, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	out, err := d.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("round trip changed the file:\n--- got\n%s\n--- want\n%s", out, in)
	}
}

func TestSetKeepsUnknownKeysOrderAndComments(t *testing.T) {
	d, err := Parse([]byte("---\nstatus: active # note\nmine: keep me\n---\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	d.Set("status", "warm")
	d.SetPlain("status_since", "2026-10-01")

	out, _ := d.Bytes()
	want := "---\nstatus: warm # note\nmine: keep me\nstatus_since: 2026-10-01\n---\nbody\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestStringsThatLookLikeOtherTypesStayStrings(t *testing.T) {
	d := New()
	d.Set("rate", "650.00")
	d.Set("client", "[[acme]]")
	d.Set("phone", "+441610000000")
	d.SetPlain("terms_days", "30")

	out, _ := d.Bytes()
	back, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"rate":       "650.00",
		"client":     "[[acme]]",
		"phone":      "+441610000000",
		"terms_days": "30",
	} {
		if got := back.Get(key); got != want {
			t.Errorf("%s = %q, want %q\n%s", key, got, want, out)
		}
	}
	if strings.Contains(string(out), "terms_days: \"30\"") {
		t.Errorf("plain value was quoted:\n%s", out)
	}
}

func TestParseShapes(t *testing.T) {
	cases := map[string]struct {
		in, body string
		keys     int
	}{
		"no frontmatter":    {"just text\n", "just text\n", 0},
		"empty frontmatter": {"---\n---\nbody\n", "body\n", 0},
		"closing at EOF":    {"---\na: 1\n---", "", 1},
		"no body":           {"---\na: 1\n---\n", "", 1},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := Parse([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if d.Body != c.body {
				t.Errorf("body = %q, want %q", d.Body, c.body)
			}
			if len(d.Keys()) != c.keys {
				t.Errorf("keys = %v, want %d", d.Keys(), c.keys)
			}
		})
	}
}

func TestUnclosedFrontmatterIsAnError(t *testing.T) {
	if _, err := Parse([]byte("---\na: 1\nbody\n")); err == nil {
		t.Fatal("want an error for unclosed frontmatter")
	}
}

func TestListAcceptsALoneScalar(t *testing.T) {
	d, _ := Parse([]byte("---\nwith: Jo\n---\n"))
	if got := d.List("with"); len(got) != 1 || got[0] != "Jo" {
		t.Fatalf("List = %v", got)
	}
}
