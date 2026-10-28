// Package views computes deterministic, cache-only research summaries.
package views

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

type Expr struct {
	op          string
	value       any
	left, right *Expr
}
type token struct{ kind, text string }

func lex(s string) ([]token, error) {
	out := []token{}
	for i := 0; i < len(s); {
		c := s[i]
		if unicode.IsSpace(rune(c)) {
			i++
			continue
		}
		// `name` is a field whose name is not a bare word (eval/acc,
		// auto-1f2e.loss): everything up to the closing backtick.
		if c == '`' {
			end := strings.IndexByte(s[i+1:], '`')
			if end <= 0 {
				return nil, fmt.Errorf("unclosed or empty `field`")
			}
			out = append(out, token{"id", s[i+1 : i+1+end]})
			i += end + 2
			continue
		}
		if c == '\'' || c == '"' {
			q := c
			i++
			var b strings.Builder
			closed := false
			for i < len(s) {
				c = s[i]
				i++
				if c == q {
					closed = true
					break
				}
				if c == '\\' {
					if i == len(s) {
						break
					}
					c = s[i]
					i++
					switch c {
					case 'n':
						c = '\n'
					case 't':
						c = '\t'
					}
				}
				b.WriteByte(c)
			}
			if !closed {
				return nil, fmt.Errorf("unclosed string")
			}
			out = append(out, token{"str", b.String()})
			continue
		}
		if c >= '0' && c <= '9' || c == '.' && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			start := i
			i++
			for i < len(s) && (s[i] >= '0' && s[i] <= '9' || strings.ContainsRune(".eE", rune(s[i])) || strings.ContainsRune("+-", rune(s[i])) && strings.ContainsRune("eE", rune(s[i-1]))) {
				i++
			}
			out = append(out, token{"num", s[start:i]})
			continue
		}
		if unicode.IsLetter(rune(c)) || c == '_' {
			start := i
			i++
			for i < len(s) && (unicode.IsLetter(rune(s[i])) || unicode.IsDigit(rune(s[i])) || strings.ContainsRune("_.:", rune(s[i]))) {
				i++
			}
			word := s[start:i]
			kind := "id"
			if word == "and" || word == "or" || word == "not" {
				kind = word
			}
			out = append(out, token{kind, word})
			continue
		}
		if i+1 < len(s) {
			op := s[i : i+2]
			if op == "==" || op == "!=" || op == "<=" || op == ">=" {
				out = append(out, token{op, op})
				i += 2
				continue
			}
		}
		if strings.ContainsRune("+-*/()<>", rune(c)) {
			out = append(out, token{string(c), string(c)})
			i++
			continue
		}
		return nil, fmt.Errorf("unexpected character %q", c)
	}
	return append(out, token{"end", ""}), nil
}

type parser struct {
	t []token
	i int
}

var precedence = map[string]int{"or": 1, "and": 2, "==": 3, "!=": 3, "<": 3, ">": 3, "<=": 3, ">=": 3, "+": 4, "-": 4, "*": 5, "/": 5}

func Parse(s string) (*Expr, error) {
	t, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := parser{t: t}
	e, err := p.parse(1)
	if err == nil && p.t[p.i].kind != "end" {
		err = fmt.Errorf("unexpected token %q", p.t[p.i].text)
	}
	return e, err
}
func (p *parser) parse(min int) (*Expr, error) {
	t := p.t[p.i]
	p.i++
	var e *Expr
	switch t.kind {
	case "num":
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil || math.IsInf(v, 0) {
			return nil, fmt.Errorf("invalid number %q", t.text)
		}
		e = &Expr{op: "literal", value: v}
	case "str":
		e = &Expr{op: "literal", value: t.text}
	case "id":
		e = &Expr{op: "field", value: t.text}
		if t.text == "true" || t.text == "false" {
			e = &Expr{op: "literal", value: t.text == "true"}
		}
		if t.text == "null" {
			e = &Expr{op: "literal"}
		}
	case "not", "+", "-":
		level := 6
		if t.kind == "not" {
			level = 3
		}
		r, err := p.parse(level)
		if err != nil {
			return nil, err
		}
		e = &Expr{op: "unary" + t.kind, right: r}
	case "(":
		var err error
		e, err = p.parse(1)
		if err != nil {
			return nil, err
		}
		if p.t[p.i].kind != ")" {
			return nil, fmt.Errorf("expected )")
		}
		p.i++
	default:
		return nil, fmt.Errorf("expected expression, got %q", t.text)
	}
	for {
		op := p.t[p.i].kind
		level := precedence[op]
		if level < min {
			break
		}
		p.i++
		r, err := p.parse(level + 1)
		if err != nil {
			return nil, err
		}
		e = &Expr{op: op, left: e, right: r}
	}
	return e, nil
}
func (e *Expr) Fields() []string {
	if e == nil {
		return nil
	}
	if e.op == "field" {
		return []string{e.value.(string)}
	}
	return append(e.left.Fields(), e.right.Fields()...)
}
func (e *Expr) Eval(m map[string]any) any {
	if e == nil {
		return nil
	}
	switch e.op {
	case "literal":
		return e.value
	case "field":
		return m[e.value.(string)]
	}
	r := e.right.Eval(m)
	switch e.op {
	case "unarynot":
		if b, ok := r.(bool); ok {
			return !b
		}
		return nil
	case "unary+":
		if n, ok := number(r); ok {
			return n
		}
		return nil
	case "unary-":
		if n, ok := number(r); ok {
			return -n
		}
		return nil
	}
	l := e.left.Eval(m)
	if e.op == "and" {
		if l == false || r == false {
			return false
		}
		if l == true && r == true {
			return true
		}
		return nil
	}
	if e.op == "or" {
		if l == true || r == true {
			return true
		}
		if l == false && r == false {
			return false
		}
		return nil
	}
	if l == nil || r == nil {
		return nil
	}
	a, an := number(l)
	b, bn := number(r)
	if e.op == "==" || e.op == "!=" {
		equal := false
		if an && bn {
			equal = a == b
		} else {
			equal = canonical(l) == canonical(r)
		}
		if e.op == "!=" {
			equal = !equal
		}
		return equal
	}
	if an && bn {
		switch e.op {
		case "+":
			return finite(a + b)
		case "-":
			return finite(a - b)
		case "*":
			return finite(a * b)
		case "/":
			if b == 0 {
				return nil
			}
			return finite(a / b)
		case "<":
			return a < b
		case ">":
			return a > b
		case "<=":
			return a <= b
		case ">=":
			return a >= b
		}
	}
	if a, ok := l.(string); ok {
		if b, ok := r.(string); ok {
			switch e.op {
			case "<":
				return a < b
			case ">":
				return a > b
			case "<=":
				return a <= b
			case ">=":
				return a >= b
			}
		}
	}
	return nil
}
func number(v any) (float64, bool) {
	if est, ok := asEst(v); ok { // an estimate computes as its value
		v = est["value"]
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
func finite(v float64) any {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	return v
}
func templateFields(s string) ([]string, error) {
	out := []string{}
	for {
		i := strings.IndexByte(s, '{')
		if i < 0 {
			if strings.Contains(s, "}") {
				return nil, fmt.Errorf("unmatched }")
			}
			return out, nil
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '}')
		if j < 0 {
			return nil, fmt.Errorf("unclosed template field")
		}
		k := s[:j]
		e, err := Parse(k)
		if err != nil || e.op != "field" {
			return nil, fmt.Errorf("template expects a field, got %q", k)
		}
		out = append(out, k)
		s = s[j+1:]
	}
}
func evalValue(s string, m map[string]any) (any, error) {
	if strings.Contains(s, "{") {
		fields, err := templateFields(s)
		if err != nil {
			return nil, err
		}
		for _, k := range fields {
			v := m[k]
			if v == nil {
				return nil, nil
			}
			s = strings.ReplaceAll(s, "{"+k+"}", Plain(v))
		}
		return s, nil
	}
	e, err := Parse(s)
	if err != nil {
		return nil, err
	}
	return e.Eval(m), nil
}
