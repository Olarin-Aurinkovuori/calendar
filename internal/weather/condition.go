package weather

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Evaluate parses and evaluates a boolean condition expression such as
// "$MIN_TEMPERATURE <= 2" or "($MIN_TEMPERATURE < 0 && $MAX_TEMPERATURE < 5)
// || ($MIN_TEMPERATURE < -10 && $MIN_SNOW > 5cm)" against the given
// variable lookup function.
//
// Grammar: a comparison is "$VARIABLE OP NUMBER", where OP is one of
// "<", "<=", ">", ">="; comparisons combine with "&&" (binds tighter) and
// "||", and parentheses group sub-expressions. NUMBER may carry a trailing
// unit suffix (e.g. "5cm", "2mm", "1.5°C") purely for readability -- it is
// parsed but not validated or converted, so it must match the variable's
// native unit (°C for temperature, mm for rainfall, cm for snow).
func Evaluate(expr string, lookup func(variable string) (float64, bool)) (bool, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return false, err
	}
	if len(tokens) == 0 {
		return false, fmt.Errorf("empty condition")
	}
	p := &parser{tokens: tokens, expr: expr, lookup: lookup}
	result, err := p.parseOr()
	if err != nil {
		return false, err
	}
	if p.pos != len(p.tokens) {
		return false, fmt.Errorf("unexpected token %q in condition %q", p.tokens[p.pos], expr)
	}
	return result, nil
}

var tokenPattern = regexp.MustCompile(`^(&&|\|\||<=|>=|<|>|\(|\)|\$[A-Za-z_][A-Za-z0-9_]*|-?\d+(?:\.\d+)?[A-Za-z%°]*)`)

// tokenize splits expr into a flat token list: "&&", "||", "(", ")", a
// comparison operator, a "$VARIABLE", or a number (optionally with a
// trailing unit suffix).
func tokenize(expr string) ([]string, error) {
	var tokens []string
	rest := strings.TrimSpace(expr)
	for rest != "" {
		m := tokenPattern.FindString(rest)
		if m == "" {
			return nil, fmt.Errorf("invalid syntax at %q in condition %q", rest, expr)
		}
		tokens = append(tokens, m)
		rest = strings.TrimSpace(rest[len(m):])
	}
	return tokens, nil
}

type parser struct {
	tokens []string
	pos    int
	expr   string
	lookup func(variable string) (float64, bool)
}

func (p *parser) peek() string {
	if p.pos >= len(p.tokens) {
		return ""
	}
	return p.tokens[p.pos]
}

func (p *parser) next() string {
	t := p.peek()
	if t != "" {
		p.pos++
	}
	return t
}

// parseOr := parseAnd ("||" parseAnd)*
func (p *parser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}
	for p.peek() == "||" {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return false, err
		}
		left = left || right
	}
	return left, nil
}

// parseAnd := parseUnit ("&&" parseUnit)*
func (p *parser) parseAnd() (bool, error) {
	left, err := p.parseUnit()
	if err != nil {
		return false, err
	}
	for p.peek() == "&&" {
		p.pos++
		right, err := p.parseUnit()
		if err != nil {
			return false, err
		}
		left = left && right
	}
	return left, nil
}

// parseUnit := "(" parseOr ")" | comparison
func (p *parser) parseUnit() (bool, error) {
	if p.peek() == "(" {
		p.pos++
		inner, err := p.parseOr()
		if err != nil {
			return false, err
		}
		if p.peek() != ")" {
			return false, fmt.Errorf("missing closing %q in condition %q", ")", p.expr)
		}
		p.pos++
		return inner, nil
	}
	return p.parseComparison()
}

// parseComparison := "$VARIABLE" OP NUMBER
func (p *parser) parseComparison() (bool, error) {
	varTok := p.next()
	if !strings.HasPrefix(varTok, "$") {
		return false, fmt.Errorf("expected a $VARIABLE in condition %q, got %q", p.expr, varTok)
	}
	variable := strings.ToUpper(varTok[1:])

	opTok := p.next()
	if opTok != "<" && opTok != "<=" && opTok != ">" && opTok != ">=" {
		return false, fmt.Errorf("expected a comparison operator (<, <=, >, >=) after %s in condition %q, got %q", varTok, p.expr, opTok)
	}

	numTok := p.next()
	threshold, err := parseNumber(numTok)
	if err != nil {
		return false, fmt.Errorf("in condition %q: %w", p.expr, err)
	}

	value, ok := p.lookup(variable)
	if !ok {
		return false, fmt.Errorf("no forecast data for %s in condition %q", varTok, p.expr)
	}

	switch opTok {
	case "<":
		return value < threshold, nil
	case "<=":
		return value <= threshold, nil
	case ">":
		return value > threshold, nil
	default: // ">="
		return value >= threshold, nil
	}
}

var numberPattern = regexp.MustCompile(`^(-?\d+(?:\.\d+)?)([A-Za-z%°]*)$`)

// parseNumber parses a NUMBER token, stripping (and ignoring) any trailing
// unit suffix such as "cm" or "mm".
func parseNumber(tok string) (float64, error) {
	m := numberPattern.FindStringSubmatch(tok)
	if m == nil {
		return 0, fmt.Errorf("invalid number %q", tok)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number %q: %w", tok, err)
	}
	return v, nil
}
