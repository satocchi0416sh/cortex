package markdown

import (
	"log/slog"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const (
	maxRichTextLen   = 2000
	maxRichTextItems = 100
	maxNestDepth     = 2
)

type Block = map[string]any

type RichText = map[string]any

func Convert(source []byte) []Block {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	reader := text.NewReader(source)
	doc := md.Parser().Parse(reader)

	var out []Block
	for child := doc.FirstChild(); child != nil; child = child.NextSibling() {
		out = append(out, convertBlock(child, source, 0)...)
	}
	return out
}

func convertBlock(n ast.Node, src []byte, depth int) []Block {
	switch v := n.(type) {
	case *ast.Heading:
		return []Block{heading(v, src)}
	case *ast.Paragraph:
		return []Block{paragraph(v, src)}
	case *ast.TextBlock:
		return []Block{paragraph(v, src)}
	case *ast.Blockquote:
		return []Block{quote(v, src, depth)}
	case *ast.FencedCodeBlock:
		return []Block{fencedCode(v, src)}
	case *ast.CodeBlock:
		return []Block{codeBlock(v, src)}
	case *ast.ThematicBreak:
		return []Block{{"object": "block", "type": "divider", "divider": map[string]any{}}}
	case *ast.List:
		return list(v, src, depth)
	case *extast.Table:
		return []Block{table(v, src)}
	case *ast.HTMLBlock:
		return []Block{paragraphFromText(string(v.Lines().Value(src)))}
	}
	return nil
}

func heading(h *ast.Heading, src []byte) Block {
	level := h.Level
	if level > 3 {
		level = 3
	}
	key := "heading_" + itoa(level)
	rt := inlineRichText(h, src)
	return Block{
		"object": "block",
		"type":   key,
		key: map[string]any{
			"rich_text": rt,
		},
	}
}

func paragraph(n ast.Node, src []byte) Block {
	rt := inlineRichText(n, src)
	if len(rt) == 0 {
		rt = []RichText{plainText("")}
	}
	return Block{
		"object": "block",
		"type":   "paragraph",
		"paragraph": map[string]any{
			"rich_text": rt,
		},
	}
}

func paragraphFromText(s string) Block {
	return Block{
		"object": "block",
		"type":   "paragraph",
		"paragraph": map[string]any{
			"rich_text": chunkPlain(s),
		},
	}
}

func quote(b *ast.Blockquote, src []byte, depth int) Block {
	var rtAccum []RichText
	var children []Block
	for c := b.FirstChild(); c != nil; c = c.NextSibling() {
		if p, ok := c.(*ast.Paragraph); ok {
			rtAccum = append(rtAccum, inlineRichText(p, src)...)
			if c.NextSibling() != nil {
				rtAccum = append(rtAccum, plainText("\n"))
			}
			continue
		}
		children = append(children, convertBlock(c, src, depth+1)...)
	}
	if len(rtAccum) == 0 {
		rtAccum = []RichText{plainText("")}
	}
	body := map[string]any{"rich_text": capRichText(rtAccum)}
	if len(children) > 0 {
		body["children"] = children
	}
	return Block{
		"object": "block",
		"type":   "quote",
		"quote":  body,
	}
}


func table(n *extast.Table, src []byte) Block {
	var rows []Block
	width := 0
	hasHeader := false

	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		var cells []ast.Node
		switch row := c.(type) {
		case *extast.TableHeader:
			hasHeader = true
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, cell)
			}
		case *extast.TableRow:
			for cell := row.FirstChild(); cell != nil; cell = cell.NextSibling() {
				cells = append(cells, cell)
			}
		default:
			continue
		}
		if len(cells) > width {
			width = len(cells)
		}
		rowCells := make([]any, 0, len(cells))
		for _, cell := range cells {
			rt := inlineRichText(cell, src)
			if rt == nil {
				rt = []RichText{}
			}
			rowCells = append(rowCells, rt)
		}
		rows = append(rows, Block{
			"object": "block",
			"type":   "table_row",
			"table_row": map[string]any{
				"cells": rowCells,
			},
		})
	}

	for _, r := range rows {
		body := r["table_row"].(map[string]any)
		cells := body["cells"].([]any)
		for len(cells) < width {
			cells = append(cells, []RichText{})
		}
		body["cells"] = cells
	}

	return Block{
		"object": "block",
		"type":   "table",
		"table": map[string]any{
			"table_width":       width,
			"has_column_header": hasHeader,
			"has_row_header":    false,
			"children":          rows,
		},
	}
}

func fencedCode(c *ast.FencedCodeBlock, src []byte) Block {
	lang := normalizeLang(string(c.Language(src)))
	body := codeText(c, src)
	return Block{
		"object": "block",
		"type":   "code",
		"code": map[string]any{
			"rich_text": chunkPlain(body),
			"language":  lang,
		},
	}
}

func codeBlock(c *ast.CodeBlock, src []byte) Block {
	body := codeText(c, src)
	return Block{
		"object": "block",
		"type":   "code",
		"code": map[string]any{
			"rich_text": chunkPlain(body),
			"language":  "plain text",
		},
	}
}

func codeText(n ast.Node, src []byte) string {
	var sb strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		sb.Write(seg.Value(src))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func list(l *ast.List, src []byte, depth int) []Block {
	var blocks []Block
	for li := l.FirstChild(); li != nil; li = li.NextSibling() {
		item, ok := li.(*ast.ListItem)
		if !ok {
			continue
		}
		blocks = append(blocks, listItem(l, item, src, depth)...)
	}
	return blocks
}

func listItem(parent *ast.List, li *ast.ListItem, src []byte, depth int) []Block {
	var rt []RichText
	var nested []Block
	checked, isTask := taskState(li)

	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		switch n := c.(type) {
		case *ast.TextBlock, *ast.Paragraph:
			rt = append(rt, inlineRichText(n, src)...)
		case *ast.List:
			if depth+1 >= maxNestDepth {
				slog.Warn("flattening deep nested list", "depth", depth+1)
				nested = append(nested, list(n, src, depth)...)
			} else {
				nested = append(nested, list(n, src, depth+1)...)
			}
		default:
			nested = append(nested, convertBlock(c, src, depth+1)...)
		}
	}
	if len(rt) == 0 {
		rt = []RichText{plainText("")}
	}

	var blockType string
	switch {
	case isTask:
		blockType = "to_do"
	case parent.IsOrdered():
		blockType = "numbered_list_item"
	default:
		blockType = "bulleted_list_item"
	}

	body := map[string]any{"rich_text": capRichText(rt)}
	if isTask {
		body["checked"] = checked
	}
	if len(nested) > 0 && depth+1 < maxNestDepth {
		body["children"] = nested
		nested = nil
	}

	out := []Block{{
		"object":  "block",
		"type":    blockType,
		blockType: body,
	}}
	out = append(out, nested...)
	return out
}

func taskState(li *ast.ListItem) (bool, bool) {
	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		if tb, ok := c.(*ast.TextBlock); ok {
			if cb, ok := tb.FirstChild().(*extast.TaskCheckBox); ok {
				return cb.IsChecked, true
			}
		}
		if p, ok := c.(*ast.Paragraph); ok {
			if cb, ok := p.FirstChild().(*extast.TaskCheckBox); ok {
				return cb.IsChecked, true
			}
		}
	}
	return false, false
}

type annotations struct {
	bold, italic, code bool
	link               string
}

func inlineRichText(n ast.Node, src []byte) []RichText {
	var out []RichText
	walkInline(n, src, annotations{}, &out)
	return capRichText(out)
}

func walkInline(n ast.Node, src []byte, ann annotations, out *[]RichText) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			s := string(v.Segment.Value(src))
			if v.SoftLineBreak() || v.HardLineBreak() {
				s += "\n"
			}
			appendChunks(out, s, ann)
		case *ast.String:
			appendChunks(out, string(v.Value), ann)
		case *ast.CodeSpan:
			next := ann
			next.code = true
			walkInline(v, src, next, out)
		case *ast.Emphasis:
			next := ann
			if v.Level >= 2 {
				next.bold = true
			} else {
				next.italic = true
			}
			walkInline(v, src, next, out)
		case *ast.Link:
			next := ann
			next.link = string(v.Destination)
			walkInline(v, src, next, out)
		case *ast.AutoLink:
			url := string(v.URL(src))
			next := ann
			next.link = url
			appendChunks(out, url, next)
		case *ast.Image:
			alt := collectInlineText(v, src)
			if alt == "" {
				alt = string(v.Destination)
			}
			next := ann
			next.link = string(v.Destination)
			appendChunks(out, alt, next)
		case *extast.TaskCheckBox:
		case *ast.RawHTML:
			appendChunks(out, rawHTMLText(v, src), ann)
		default:
			walkInline(c, src, ann, out)
		}
	}
}

func collectInlineText(n ast.Node, src []byte) string {
	var sb strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			sb.Write(t.Segment.Value(src))
		} else if s, ok := c.(*ast.String); ok {
			sb.Write(s.Value)
		} else {
			sb.WriteString(collectInlineText(c, src))
		}
	}
	return sb.String()
}

func rawHTMLText(n *ast.RawHTML, src []byte) string {
	var sb strings.Builder
	for i := 0; i < n.Segments.Len(); i++ {
		seg := n.Segments.At(i)
		sb.Write(seg.Value(src))
	}
	return sb.String()
}

func appendChunks(out *[]RichText, s string, ann annotations) {
	if s == "" {
		return
	}
	for _, chunk := range splitRunes(s, maxRichTextLen) {
		*out = append(*out, richTextFor(chunk, ann))
	}
}

func richTextFor(s string, ann annotations) RichText {
	textObj := map[string]any{"content": s}
	if ann.link != "" {
		textObj["link"] = map[string]any{"url": ann.link}
	}
	rt := RichText{
		"type": "text",
		"text": textObj,
	}
	annot := map[string]any{}
	if ann.bold {
		annot["bold"] = true
	}
	if ann.italic {
		annot["italic"] = true
	}
	if ann.code {
		annot["code"] = true
	}
	if len(annot) > 0 {
		rt["annotations"] = annot
	}
	return rt
}

func plainText(s string) RichText {
	return richTextFor(s, annotations{})
}

func chunkPlain(s string) []RichText {
	if s == "" {
		return []RichText{plainText("")}
	}
	chunks := splitRunes(s, maxRichTextLen)
	out := make([]RichText, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, plainText(c))
	}
	return capRichText(out)
}

func splitRunes(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var chunks []string
	runes := []rune(s)
	for i := 0; i < len(runes); i += max {
		end := i + max
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[i:end]))
	}
	return chunks
}

func capRichText(in []RichText) []RichText {
	if len(in) <= maxRichTextItems {
		return in
	}
	slog.Warn("rich_text array exceeded limit, truncating", "had", len(in), "max", maxRichTextItems)
	return in[:maxRichTextItems]
}

func normalizeLang(in string) string {
	in = strings.ToLower(strings.TrimSpace(in))
	if in == "" {
		return "plain text"
	}
	switch in {
	case "sh", "shell", "bash", "zsh", "ksh":
		in = "shell"
	case "js", "javascript", "node", "nodejs":
		in = "javascript"
	case "ts", "typescript":
		in = "typescript"
	case "py", "python", "python3":
		in = "python"
	case "rb":
		in = "ruby"
	case "rs":
		in = "rust"
	case "yml":
		in = "yaml"
	case "md", "mdx":
		in = "markdown"
	case "text", "plain", "txt", "log", "env", "dotenv", "ini", "conf", "config", "properties":
		in = "plain text"
	case "tsx":
		in = "typescript"
	case "jsx":
		in = "javascript"
	case "dockerfile":
		in = "docker"
	case "makefile", "mk":
		in = "makefile"
	case "cs":
		in = "c#"
	case "cpp", "cxx":
		in = "c++"
	case "objc", "objectivec":
		in = "objective-c"
	case "vb", "vbnet":
		in = "vb.net"
	case "ps", "ps1", "powershell":
		in = "powershell"
	}
	if _, ok := notionLangs[in]; ok {
		return in
	}
	return "plain text"
}


var notionLangs = map[string]struct{}{
	"abap": {}, "abc": {}, "agda": {}, "arduino": {}, "ascii art": {},
	"assembly": {}, "bash": {}, "basic": {}, "bnf": {}, "c": {}, "c#": {},
	"c++": {}, "clojure": {}, "coffeescript": {}, "coq": {}, "css": {},
	"dart": {}, "dhall": {}, "diff": {}, "docker": {}, "ebnf": {}, "elixir": {},
	"elm": {}, "erlang": {}, "f#": {}, "flow": {}, "fortran": {}, "gherkin": {},
	"glsl": {}, "go": {}, "graphql": {}, "groovy": {}, "haskell": {}, "hcl": {},
	"html": {}, "idris": {}, "java": {}, "javascript": {}, "json": {},
	"julia": {}, "kotlin": {}, "latex": {}, "less": {}, "lisp": {},
	"livescript": {}, "llvm ir": {}, "lua": {}, "makefile": {}, "markdown": {},
	"markup": {}, "matlab": {}, "mathematica": {}, "mermaid": {}, "nix": {},
	"notion formula": {}, "objective-c": {}, "ocaml": {}, "pascal": {},
	"perl": {}, "php": {}, "plain text": {}, "powershell": {}, "prolog": {},
	"protobuf": {}, "purescript": {}, "python": {}, "r": {}, "racket": {},
	"reason": {}, "ruby": {}, "rust": {}, "sass": {}, "scala": {}, "scheme": {},
	"scss": {}, "shell": {}, "smalltalk": {}, "solidity": {}, "sparql": {},
	"sql": {}, "swift": {}, "toml": {}, "turtle": {}, "twig": {},
	"typescript": {}, "vb.net": {}, "verilog": {}, "vhdl": {}, "visual basic": {},
	"webassembly": {}, "wolfram": {}, "xml": {}, "yaml": {},
	"java/c/c++/c#": {},
}

func itoa(i int) string {
	switch i {
	case 1:
		return "1"
	case 2:
		return "2"
	case 3:
		return "3"
	}
	return "1"
}
