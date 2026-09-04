package domain

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchOpts controls query compilation for FTS5 search.
type SearchOpts struct {
	Fuzzy bool // apply fuzzy expansion to all bare terms
}

const maxFuzzyVariants = 20

// CompileSearchQuery parses a user query and returns an FTS5 MATCH string.
//
// Syntax:
//   - implicit AND:       term1 term2
//   - explicit AND:       term1 + term2
//   - OR:                 term1 | term2   or   term1 OR term2
//   - grouping:           (expr)
//   - phrase:             "exact phrase"
//   - NOT:                -term
//   - per-term fuzzy:     term~
//   - global fuzzy:       SearchOpts.Fuzzy or leading ~ on the whole query
func CompileSearchQuery(raw string, opts SearchOpts) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.HasPrefix(raw, "~") {
		opts.Fuzzy = true
		raw = strings.TrimSpace(raw[1:])
		if raw == "" {
			return "", fmt.Errorf("empty query after ~")
		}
	}

	toks, err := lexSearchQuery(raw)
	if err != nil {
		return "", err
	}
	ast, err := parseSearchQuery(toks)
	if err != nil {
		return "", err
	}
	out := compileSearchNode(ast, opts)
	if out == "" {
		return "", fmt.Errorf("empty query")
	}
	return out, nil
}

type sqTokKind int

const (
	sqWord sqTokKind = iota
	sqQuoted
	sqLParen
	sqRParen
	sqPlus
	sqOr
	sqMinus
	sqEOF
)

type sqTok struct {
	kind  sqTokKind
	value string
	fuzzy bool
}

func lexSearchQuery(input string) ([]sqTok, error) {
	var toks []sqTok
	i := 0
	for i < len(input) {
		r, w := utf8.DecodeRuneInString(input[i:])
		if unicode.IsSpace(r) {
			i += w
			continue
		}
		switch r {
		case '(':
			toks = append(toks, sqTok{kind: sqLParen})
			i += w
		case ')':
			toks = append(toks, sqTok{kind: sqRParen})
			i += w
		case '+':
			toks = append(toks, sqTok{kind: sqPlus})
			i += w
		case '|':
			toks = append(toks, sqTok{kind: sqOr})
			i += w
		case '"':
			quoted, n, err := lexQuoted(input[i:])
			if err != nil {
				return nil, err
			}
			toks = append(toks, sqTok{kind: sqQuoted, value: quoted})
			i += n
		case '-':
			if isTermStart(input, i+w) {
				toks = append(toks, sqTok{kind: sqMinus})
				i += w
			} else {
				word, fuzzy, n := lexWord(input[i:])
				if word == "" {
					return nil, fmt.Errorf("unexpected '-' at position %d", i)
				}
				toks = append(toks, sqTok{kind: sqWord, value: word, fuzzy: fuzzy})
				i += n
			}
		default:
			if r == '~' {
				return nil, fmt.Errorf("unexpected '~' at position %d (suffix fuzzy with term~)", i)
			}
			if !isTermRune(r) {
				return nil, fmt.Errorf("unexpected %q at position %d", r, i)
			}
			word, fuzzy, n := lexWord(input[i:])
			if word == "" {
				return nil, fmt.Errorf("unexpected %q at position %d", r, i)
			}
			upper := strings.ToUpper(word)
			if upper == "OR" && len(toks) > 0 && isOperandTok(toks[len(toks)-1]) {
				toks = append(toks, sqTok{kind: sqOr})
			} else if upper == "AND" && len(toks) > 0 && isOperandTok(toks[len(toks)-1]) {
				toks = append(toks, sqTok{kind: sqPlus})
			} else if upper == "NOT" && len(toks) > 0 && isOperandTok(toks[len(toks)-1]) {
				toks = append(toks, sqTok{kind: sqMinus})
			} else {
				toks = append(toks, sqTok{kind: sqWord, value: word, fuzzy: fuzzy})
			}
			i += n
		}
	}
	toks = append(toks, sqTok{kind: sqEOF})
	return toks, nil
}

func isOperandTok(t sqTok) bool {
	switch t.kind {
	case sqWord, sqQuoted, sqRParen:
		return true
	default:
		return false
	}
}
func isTermStart(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isTermRune(r)
}

func isTermRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '/' || r == ':' || r == '-' || r == '#'
}

func lexQuoted(s string) (string, int, error) {
	if !strings.HasPrefix(s, `"`) {
		return "", 0, fmt.Errorf("expected quote")
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		if s[i] == '"' {
			if i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", 0, fmt.Errorf("unterminated phrase")
}

func lexWord(s string) (word string, fuzzy bool, n int) {
	i := 0
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) || r == '(' || r == ')' || r == '+' || r == '|' || r == '"' {
			break
		}
		if r == '~' {
			fuzzy = true
			return s[:i], fuzzy, i + w
		}
		if !isTermRune(r) {
			break
		}
		i += w
	}
	return s[:i], fuzzy, i
}

type sqNodeKind int

const (
	sqNTerm sqNodeKind = iota
	sqNAnd
	sqNOr
	sqNNot
)

type sqNode struct {
	kind   sqNodeKind
	value  string
	fuzzy  bool
	quoted bool
	kids   []*sqNode
}

type sqParser struct {
	toks []sqTok
	pos  int
}

func parseSearchQuery(toks []sqTok) (*sqNode, error) {
	p := &sqParser{toks: toks}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != sqEOF {
		return nil, fmt.Errorf("unexpected %q after expression", p.peek().value)
	}
	return n, nil
}

func (p *sqParser) peek() sqTok { return p.toks[p.pos] }
func (p *sqParser) next() sqTok {
	t := p.toks[p.pos]
	p.pos++
	return t
}

func (p *sqParser) parseOr() (*sqNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == sqOr {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &sqNode{kind: sqNOr, kids: []*sqNode{left, right}}
	}
	return left, nil
}

func (p *sqParser) parseAnd() (*sqNode, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case sqPlus:
			p.next()
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &sqNode{kind: sqNAnd, kids: []*sqNode{left, right}}
		case sqWord, sqQuoted, sqLParen, sqMinus:
			right, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			left = &sqNode{kind: sqNAnd, kids: []*sqNode{left, right}}
		default:
			return left, nil
		}
	}
}

func (p *sqParser) parseUnary() (*sqNode, error) {
	if p.peek().kind == sqMinus {
		p.next()
		child, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &sqNode{kind: sqNNot, kids: []*sqNode{child}}, nil
	}
	return p.parsePrimary()
}

func (p *sqParser) parsePrimary() (*sqNode, error) {
	switch p.peek().kind {
	case sqLParen:
		p.next()
		inner, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != sqRParen {
			return nil, fmt.Errorf("expected ')'")
		}
		p.next()
		return inner, nil
	case sqQuoted:
		t := p.next()
		return &sqNode{kind: sqNTerm, value: t.value, quoted: true}, nil
	case sqWord:
		t := p.next()
		return &sqNode{kind: sqNTerm, value: t.value, fuzzy: t.fuzzy}, nil
	default:
		return nil, fmt.Errorf("expected term, phrase, or '('")
	}
}

func compileSearchNode(n *sqNode, opts SearchOpts) string {
	switch n.kind {
	case sqNTerm:
		return compileSearchTerm(n.value, n.quoted, n.fuzzy || opts.Fuzzy)
	case sqNAnd:
		parts := make([]string, len(n.kids))
		for i, c := range n.kids {
			parts[i] = wrapGroup(compileSearchNode(c, opts))
		}
		return strings.Join(parts, " ")
	case sqNOr:
		parts := make([]string, len(n.kids))
		for i, c := range n.kids {
			parts[i] = wrapGroup(compileSearchNode(c, opts))
		}
		return "(" + strings.Join(parts, " OR ") + ")"
	case sqNNot:
		return "NOT " + wrapGroup(compileSearchNode(n.kids[0], opts))
	default:
		return ""
	}
}

func wrapGroup(s string) string {
	if strings.Contains(s, " OR ") || strings.HasPrefix(s, "NOT ") {
		return "(" + s + ")"
	}
	return s
}

func compileSearchTerm(term string, quoted, fuzzy bool) string {
	if quoted {
		return quoteFTSTerm(term)
	}
	if !fuzzy {
		return quoteFTSTerm(term)
	}
	variants := fuzzyTermVariants(term)
	if len(variants) == 1 {
		return variants[0]
	}
	return "(" + strings.Join(variants, " OR ") + ")"
}

func quoteFTSTerm(term string) string {
	if term == "" {
		return `""`
	}
	upper := strings.ToUpper(term)
	if upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
		return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	if strings.IndexFunc(term, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '(' || r == ')' || r == '*' 
	}) >= 0 {
		return `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	return term
}

func quoteOrPrefix(v string) string {
	if strings.HasSuffix(v, "*") {
		base := strings.TrimSuffix(v, "*")
		return quoteFTSTerm(base) + "*"
	}
	return quoteFTSTerm(v)
}

// fuzzyTermVariants returns exact, prefix, and edit-distance-1 variants for FTS OR groups.
func fuzzyTermVariants(term string) []string {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		if len([]rune(s)) < 2 && !strings.HasSuffix(s, "*") {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		if len(seen) >= maxFuzzyVariants {
			return
		}
		seen[s] = struct{}{}
		out = append(out, quoteOrPrefix(s))
	}

	add(term)
	runes := []rune(term)
	if len(runes) >= 3 {
		add(term + "*")
	}
	if len(runes) < 4 || len(runes) > 16 {
		return out
	}

	for i := range runes {
		add(string(append([]rune(nil), runes[:i]...)) + string(runes[i+1:]))
	}
	for i, r := range runes {
		if !unicode.IsLetter(r) {
			continue
		}
		base := rune('a')
		if unicode.IsUpper(r) {
			base = 'A'
		}
		for c := base; c <= base+25; c++ {
			if c == r {
				continue
			}
			mut := append([]rune(nil), runes...)
			mut[i] = c
			add(string(mut))
		}
	}
	return out
}
