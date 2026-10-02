package web

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/JustSteveKing/mavis/internal/record"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// md renders record bodies. Raw HTML in a body is shown as text, the way
// you typed it, and javascript: links are dropped (goldmark's default
// without WithUnsafe), so a note cannot put script on the page.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(asText{}, 100))),
)

// asText renders HTML in a body as escaped text. goldmark's own default
// drops it, which loses whatever you wrote after a stray <tag>.
type asText struct{}

func (asText) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHTMLBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		b := n.(*ast.HTMLBlock)
		w.WriteString("<p>")
		for i := 0; i < b.Lines().Len(); i++ {
			line := b.Lines().At(i)
			w.WriteString(html.EscapeString(string(line.Value(src))))
		}
		if b.HasClosure() {
			w.WriteString(html.EscapeString(string(b.ClosureLine.Value(src))))
		}
		w.WriteString("</p>\n")
		return ast.WalkContinue, nil
	})
	reg.Register(ast.KindRawHTML, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		r := n.(*ast.RawHTML)
		for i := 0; i < r.Segments.Len(); i++ {
			seg := r.Segments.At(i)
			w.WriteString(html.EscapeString(string(seg.Value(src))))
		}
		return ast.WalkSkipChildren, nil
	})
}

var wikilink = regexp.MustCompile(`\[\[([^\[\]|#]+)(?:#[^\[\]|]*)?(?:\|([^\[\]]+))?\]\]`)

// markdown renders a body, turning [[wikilinks]] into links that /go/
// resolves. Fenced code is left as it is.
func markdown(body string) template.HTML {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	var out strings.Builder
	fenced := false
	for line := range strings.SplitAfterSeq(body, "\n") {
		if t := strings.TrimSpace(line); strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fenced = !fenced
		}
		if !fenced {
			line = wikilink.ReplaceAllStringFunc(line, func(m string) string {
				parts := wikilink.FindStringSubmatch(m)
				target := strings.TrimSpace(parts[1])
				label := strings.TrimSpace(parts[2])
				if label == "" {
					label = target
				}
				name := target[strings.LastIndex(target, "/")+1:]
				return "[" + label + "](/go/" + url.PathEscape(name) + ")"
			})
		}
		out.WriteString(line)
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(out.String()), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(body))
	}
	return template.HTML(buf.String())
}

// bodyOf reads a record's body, without its frontmatter.
func bodyOf(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	d, err := record.Parse(data)
	if err != nil {
		return "", err
	}
	return d.Body, nil
}
