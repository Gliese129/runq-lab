package views

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type templateNode struct {
	text, path, kind, format string
	children                 []templateNode
	raw                      bool
}

func parseTemplate(text string) ([]templateNode, error) {
	i := 0
	var parse func(string) ([]templateNode, error)
	parse = func(end string) ([]templateNode, error) {
		nodes := []templateNode{}
		for i < len(text) {
			j := strings.Index(text[i:], "{{")
			if j < 0 {
				nodes = append(nodes, templateNode{text: text[i:]})
				i = len(text)
				break
			}
			j += i
			if j > i {
				nodes = append(nodes, templateNode{text: text[i:j]})
			}
			raw := strings.HasPrefix(text[j:], "{{{")
			open, close := 2, "}}"
			if raw {
				open = 3
				close = "}}}"
			}
			k := strings.Index(text[j+open:], close)
			if k < 0 {
				return nil, fmt.Errorf("unclosed template tag")
			}
			tag := strings.TrimSpace(text[j+open : j+open+k])
			i = j + open + k + len(close)
			if strings.HasPrefix(tag, "/") {
				if tag[1:] != end {
					return nil, fmt.Errorf("unexpected closing block %q", tag)
				}
				return nodes, nil
			}
			node := templateNode{kind: "value", path: tag, raw: raw}
			if strings.HasPrefix(tag, "#each ") || strings.HasPrefix(tag, "#if ") {
				parts := strings.SplitN(tag[1:], " ", 2)
				node.kind = parts[0]
				node.path = strings.TrimSpace(parts[1])
				var err error
				node.children, err = parse(node.kind)
				if err != nil {
					return nil, err
				}
			} else if strings.HasPrefix(tag, "fmt ") {
				parts := strings.Fields(tag)
				if len(parts) != 3 {
					return nil, fmt.Errorf("fmt expects path and quoted format")
				}
				f, err := strconv.Unquote(parts[2])
				if err != nil || !validFormat(f) {
					return nil, fmt.Errorf("invalid fmt format")
				}
				node.path = parts[1]
				node.format = f
			}
			if node.path == "" {
				return nil, fmt.Errorf("empty template path")
			}
			nodes = append(nodes, node)
		}
		if end != "" {
			return nil, fmt.Errorf("unclosed %s block", end)
		}
		return nodes, nil
	}
	return parse("")
}
func ValidateTemplate(text string) error { _, err := parseTemplate(text); return err }
func RenderTemplate(text, format string, r *Result) (string, error) {
	nodes, err := parseTemplate(text)
	if err != nil {
		return "", err
	}
	raw, _ := json.Marshal(r)
	root := map[string]any{}
	_ = json.Unmarshal(raw, &root)
	root["now"] = float64(r.GeneratedAt)
	lookup := func(path string, scope map[string]any) (any, error) {
		var value any = scope
		if path == "this" || path == "." {
			return scope["this"], nil
		}
		for _, p := range strings.Split(path, ".") {
			switch m := value.(type) {
			case map[string]any:
				v, ok := m[p]
				if !ok {
					return nil, fmt.Errorf("unknown path %q; available: %v", path, keys(m))
				}
				value = v
			case []any:
				i, e := strconv.Atoi(p)
				if e != nil || i < 0 || i >= len(m) {
					return nil, fmt.Errorf("invalid array path %q", path)
				}
				value = m[i]
			default:
				return nil, fmt.Errorf("unknown path %q", path)
			}
		}
		return value, nil
	}
	var b strings.Builder
	var render func([]templateNode, map[string]any) error
	render = func(nodes []templateNode, scope map[string]any) error {
		for _, n := range nodes {
			if n.kind == "" {
				b.WriteString(n.text)
				continue
			}
			v, err := lookup(n.path, scope)
			if err != nil {
				return err
			}
			switch n.kind {
			case "each":
				list, ok := v.([]any)
				if !ok {
					return fmt.Errorf("each %s requires an array", n.path)
				}
				for i, item := range list {
					child := clone(scope)
					child["this"] = item
					child["index"] = float64(i)
					if obj, ok := item.(map[string]any); ok {
						for k, v := range obj {
							child[k] = v
						}
					}
					if err := render(n.children, child); err != nil {
						return err
					}
				}
			case "if":
				truth := v != nil && v != false && v != ""
				if a, ok := v.([]any); ok {
					truth = len(a) > 0
				}
				if truth {
					if err := render(n.children, scope); err != nil {
						return err
					}
				}
			default:
				s, err := Format(v, n.format, r.GeneratedAt)
				if err != nil {
					return err
				}
				if !n.raw {
					if format == "latex" {
						s = latexEscape(s)
					} else {
						s = mdEscape(s)
					}
				}
				b.WriteString(s)
			}
			if b.Len() > 10<<20 {
				return fmt.Errorf("rendered template exceeds 10 MB")
			}
		}
		return nil
	}
	err = render(nodes, root)
	return b.String(), err
}
