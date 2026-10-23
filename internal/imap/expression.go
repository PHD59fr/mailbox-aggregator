package imap

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	protocol "github.com/emersion/go-imap/v2"
)

var ErrSearchExpression = errors.New("invalid boolean search expression")

type searchToken struct {
	text    string
	literal bool
}

// ParseSearchExpression accepts a small, bounded query language, never raw IMAP.
// Adjacency means AND; NOT binds before AND, which binds before OR.
func ParseSearchExpression(value string) (*protocol.SearchCriteria, error) {
	if len(value) > 2048 || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") {
		return nil, ErrSearchExpression
	}
	var tokens []searchToken
	runes := []rune(value)
	for i := 0; i < len(runes); {
		if unicode.IsSpace(runes[i]) {
			i++
			continue
		}
		if runes[i] == '(' || runes[i] == ')' {
			tokens = append(tokens, searchToken{text: string(runes[i])})
			i++
		} else {
			var term strings.Builder
			literal := false
			for i < len(runes) && !unicode.IsSpace(runes[i]) && runes[i] != '(' && runes[i] != ')' {
				if runes[i] != '"' {
					term.WriteRune(runes[i])
					i++
					continue
				}
				literal = true
				i++
				for i < len(runes) && runes[i] != '"' {
					if runes[i] == '\\' {
						i++
						if i >= len(runes) || (runes[i] != '"' && runes[i] != '\\') {
							return nil, ErrSearchExpression
						}
					}
					term.WriteRune(runes[i])
					i++
				}
				if i >= len(runes) {
					return nil, ErrSearchExpression
				}
				i++
			}
			if term.Len() == 0 || term.Len() > 512 {
				return nil, ErrSearchExpression
			}
			tokens = append(tokens, searchToken{text: term.String(), literal: literal})
		}
		if len(tokens) > 64 {
			return nil, ErrSearchExpression
		}
	}
	if len(tokens) == 0 {
		return &protocol.SearchCriteria{}, nil
	}
	p := expressionParser{tokens: tokens}
	criteria, err := p.or(0)
	if err != nil || p.pos != len(tokens) {
		return nil, ErrSearchExpression
	}
	return &criteria, nil
}

type expressionParser struct {
	tokens     []searchToken
	pos, terms int
}

func (p *expressionParser) is(value string) bool {
	return p.pos < len(p.tokens) && !p.tokens[p.pos].literal && strings.EqualFold(p.tokens[p.pos].text, value)
}
func (p *expressionParser) or(depth int) (protocol.SearchCriteria, error) {
	left, err := p.and(depth)
	for err == nil && p.is("OR") {
		p.pos++
		var right protocol.SearchCriteria
		right, err = p.and(depth)
		left = protocol.SearchCriteria{Or: [][2]protocol.SearchCriteria{{left, right}}}
	}
	return left, err
}
func (p *expressionParser) and(depth int) (protocol.SearchCriteria, error) {
	left, err := p.atom(depth)
	for err == nil && p.pos < len(p.tokens) && !p.is("OR") && !p.is(")") {
		if p.is("AND") {
			p.pos++
		}
		var right protocol.SearchCriteria
		right, err = p.atom(depth)
		left.And(&right)
	}
	return left, err
}
func (p *expressionParser) atom(depth int) (protocol.SearchCriteria, error) {
	var empty protocol.SearchCriteria
	if depth > 8 || p.pos >= len(p.tokens) {
		return empty, ErrSearchExpression
	}
	if p.is("NOT") {
		p.pos++
		child, err := p.atom(depth + 1)
		return protocol.SearchCriteria{Not: []protocol.SearchCriteria{child}}, err
	}
	if p.is("(") {
		p.pos++
		child, err := p.or(depth + 1)
		if err != nil || !p.is(")") {
			return empty, ErrSearchExpression
		}
		p.pos++
		return child, nil
	}
	if p.is(")") || p.is("AND") || p.is("OR") {
		return empty, ErrSearchExpression
	}
	term := p.tokens[p.pos].text
	p.pos++
	p.terms++
	if p.terms > 32 {
		return empty, ErrSearchExpression
	}
	field, value, hasField := strings.Cut(term, ":")
	if !hasField {
		return protocol.SearchCriteria{Text: []string{term}}, nil
	}
	if value == "" {
		return empty, ErrSearchExpression
	}
	switch strings.ToLower(field) {
	case "text":
		return protocol.SearchCriteria{Text: []string{value}}, nil
	case "subject", "from", "to", "cc":
		return protocol.SearchCriteria{Header: []protocol.SearchCriteriaHeaderField{{Key: field, Value: value}}}, nil
	case "is":
		switch strings.ToLower(value) {
		case "read":
			return protocol.SearchCriteria{Flag: []protocol.Flag{protocol.FlagSeen}}, nil
		case "unread":
			return protocol.SearchCriteria{NotFlag: []protocol.Flag{protocol.FlagSeen}}, nil
		case "starred":
			return protocol.SearchCriteria{Flag: []protocol.Flag{protocol.FlagFlagged}}, nil
		}
	}
	return empty, ErrSearchExpression
}
