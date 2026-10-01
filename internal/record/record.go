// Package record reads and writes a Markdown file with YAML frontmatter.
//
// The promise this package exists to keep: a file mavis rewrites loses
// nothing a human put there. Frontmatter is held as a yaml.Node rather than
// decoded into a struct, so keys mavis does not know about, their order and
// their comments all survive a rewrite. The body is kept byte for byte.
package record

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const delimiter = "---"

// Document is one record: frontmatter plus body.
type Document struct {
	front *yaml.Node // always a mapping node
	Body  string
}

// New returns an empty document.
func New() *Document {
	return &Document{front: &yaml.Node{Kind: yaml.MappingNode}}
}

// Parse reads a document. A file with no frontmatter is valid: it is all
// body, with empty frontmatter.
func Parse(data []byte) (*Document, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")

	if !strings.HasPrefix(text, delimiter+"\n") {
		d := New()
		d.Body = text
		return d, nil
	}

	rest := text[len(delimiter)+1:]
	var front, body string
	switch {
	case strings.HasPrefix(rest, delimiter+"\n"):
		front, body = "", rest[len(delimiter)+1:]
	case rest == delimiter:
		front, body = "", ""
	default:
		end := strings.Index(rest, "\n"+delimiter+"\n")
		if end >= 0 {
			front, body = rest[:end+1], rest[end+len(delimiter)+2:]
		} else if strings.HasSuffix(rest, "\n"+delimiter) {
			front, body = rest[:len(rest)-len(delimiter)], ""
		} else {
			return nil, errors.New("frontmatter is not closed with ---")
		}
	}

	d := New()
	d.Body = body
	if strings.TrimSpace(front) == "" {
		return d, nil
	}

	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(front), &doc); err != nil {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	if len(doc.Content) == 0 {
		return d, nil
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter is not a set of key: value pairs")
	}
	d.front = doc.Content[0]
	return d, nil
}

// Bytes renders the document.
func (d *Document) Bytes() ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(delimiter + "\n")
	if len(d.front.Content) > 0 {
		enc := yaml.NewEncoder(&b)
		enc.SetIndent(2)
		if err := enc.Encode(d.front); err != nil {
			return nil, err
		}
		if err := enc.Close(); err != nil {
			return nil, err
		}
	}
	b.WriteString(delimiter + "\n")
	b.WriteString(d.Body)
	return b.Bytes(), nil
}

// value returns the node holding key's value, or nil.
func (d *Document) value(key string) *yaml.Node {
	c := d.front.Content
	for i := 0; i+1 < len(c); i += 2 {
		if c[i].Value == key {
			return c[i+1]
		}
	}
	return nil
}

// Has reports whether key is present, whatever its value.
func (d *Document) Has(key string) bool { return d.value(key) != nil }

// Get returns a scalar value as written, or "" when absent, null or not a
// scalar.
func (d *Document) Get(key string) string {
	v := d.value(key)
	if v == nil || v.Kind != yaml.ScalarNode || v.Tag == "!!null" {
		return ""
	}
	return v.Value
}

// List returns a sequence of scalars. A lone scalar reads as a one-item
// list, since that is what a hand-edited `with: Jo` means.
func (d *Document) List(key string) []string {
	v := d.value(key)
	if v == nil {
		return nil
	}
	switch v.Kind {
	case yaml.ScalarNode:
		if v.Value == "" || v.Tag == "!!null" {
			return nil
		}
		return []string{v.Value}
	case yaml.SequenceNode:
		out := make([]string, 0, len(v.Content))
		for _, item := range v.Content {
			if item.Kind == yaml.ScalarNode {
				out = append(out, item.Value)
			}
		}
		return out
	}
	return nil
}

// Set writes a string value, quoted only where YAML would otherwise read it
// as something else: "650.00" stays a string, not a float.
func (d *Document) Set(key, value string) { d.setScalar(key, value, "!!str") }

// SetPlain writes a value unquoted and lets YAML resolve its type. Dates use
// this so Obsidian reads them as dates rather than text.
func (d *Document) SetPlain(key, value string) { d.setScalar(key, value, "") }

func (d *Document) setScalar(key, value, tag string) {
	if v := d.value(key); v != nil {
		*v = yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag, LineComment: v.LineComment}
		return
	}
	d.front.Content = append(d.front.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value, Tag: tag},
	)
}

// SetList writes a list of strings in flow style, [a, b], which is how a
// person would type it.
func (d *Document) SetList(key string, values []string) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, s := range values {
		seq.Content = append(seq.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: s, Tag: "!!str"})
	}
	if v := d.value(key); v != nil {
		seq.LineComment = v.LineComment
		*v = *seq
		return
	}
	d.front.Content = append(d.front.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, seq)
}

// Keys returns the frontmatter keys in file order.
func (d *Document) Keys() []string {
	var keys []string
	c := d.front.Content
	for i := 0; i+1 < len(c); i += 2 {
		keys = append(keys, c[i].Value)
	}
	return keys
}
