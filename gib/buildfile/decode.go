package buildfile

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// A build file's YAML is read into Jib's spec classes as Jackson binds
// them: each class takes only the properties its creator names and
// requires those it marks required, and a property is null when it is
// absent or written as null. A scalar is the text written, an alias the
// value it names, and a key may appear once in a mapping, as YAML has it.

// resolved is n with its aliases followed.
func resolved(n *yaml.Node) *yaml.Node {
	for n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	return n
}

// isNull is a YAML null: nothing, "~", "null" or a !!null tag.
func isNull(n *yaml.Node) bool {
	n = resolved(n)
	if n.Kind != yaml.ScalarNode {
		return false
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return n.Tag == "!!null"
	}
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return false
	}
	switch n.Value {
	case "", "~", "null", "Null", "NULL":
		return true
	}
	return false
}

func kindOf(n *yaml.Node) string {
	switch resolved(n).Kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "an object"
	}
	return "a scalar"
}

func mismatch(want string, n *yaml.Node) error {
	return fmt.Errorf("expected %s, got %s", want, kindOf(n))
}

// str reads a string property: nil when null.
func str(n *yaml.Node) (*string, error) {
	n = resolved(n)
	if isNull(n) {
		return nil, nil
	}
	if n.Kind != yaml.ScalarNode {
		return nil, mismatch("a string", n)
	}
	v := n.Value
	return &v, nil
}

// strs reads a list of strings, its null entries nil; false when null.
func strs(n *yaml.Node) ([]*string, bool, error) {
	n = resolved(n)
	if isNull(n) {
		return nil, false, nil
	}
	if n.Kind != yaml.SequenceNode {
		return nil, false, mismatch("a list", n)
	}
	out := []*string{}
	for i, e := range n.Content {
		v, err := str(e)
		if err != nil {
			return nil, false, fmt.Errorf("[%d]: %w", i, err)
		}
		out = append(out, v)
	}
	return out, true, nil
}

type entry struct {
	key   string
	value *string
}

// strMap reads a map of strings, in the order written; false when null.
func strMap(n *yaml.Node) ([]entry, bool, error) {
	n = resolved(n)
	if isNull(n) {
		return nil, false, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, false, mismatch("an object", n)
	}
	keys, err := mapKeys(n)
	if err != nil {
		return nil, false, err
	}
	out := []entry{}
	for i, k := range keys {
		v, err := str(n.Content[2*i+1])
		if err != nil {
			return nil, false, fmt.Errorf("%s: %w", k, err)
		}
		out = append(out, entry{k, v})
	}
	return out, true, nil
}

// mapKeys are a mapping's keys, each a scalar and each once.
func mapKeys(n *yaml.Node) ([]string, error) {
	var keys []string
	at := map[string]int{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := resolved(n.Content[i])
		if k.Kind != yaml.ScalarNode {
			return nil, fmt.Errorf("line %d: a key must be a scalar", k.Line)
		}
		if line, ok := at[k.Value]; ok {
			return nil, fmt.Errorf("line %d: mapping key %q already defined at line %d", k.Line, k.Value, line)
		}
		at[k.Value] = k.Line
		keys = append(keys, k.Value)
	}
	return keys, nil
}

// prop is a property of a class's creator, in the creator's order.
type prop struct {
	name     string
	required bool
	set      func(*yaml.Node) error
}

// object reads a class through its creator's properties: false for
// null, and refused when it is not an object, has a property the class
// does not know, or lacks a required one.
func object(n *yaml.Node, class string, props ...prop) (bool, error) {
	n = resolved(n)
	if isNull(n) {
		return false, nil
	}
	if n.Kind != yaml.MappingNode {
		return false, mismatch("an object", n)
	}
	keys, err := mapKeys(n)
	if err != nil {
		return false, err
	}
	seen := map[string]bool{}
	for i, k := range keys {
		var p *prop
		for j := range props {
			if props[j].name == k {
				p = &props[j]
			}
		}
		if p == nil {
			var names []string
			for _, q := range props {
				names = append(names, q.name)
			}
			return false, fmt.Errorf("Unrecognized field %q (class %s), not marked as ignorable (known properties: %s)", k, class, strings.Join(names, ", ")) //nolint:staticcheck // Jackson's message
		}
		if err := p.set(n.Content[2*i+1]); err != nil {
			return false, fmt.Errorf("%s: %w", k, err)
		}
		seen[k] = true
	}
	for _, p := range props {
		if p.required && !seen[p.name] {
			return false, fmt.Errorf("Missing required creator property '%s' (class %s)", p.name, class) //nolint:staticcheck // Jackson's message
		}
	}
	return true, nil
}

// list reads a list of objects, a null entry passed as nil; false when
// null.
func list(n *yaml.Node, each func(*yaml.Node) error) (bool, error) {
	n = resolved(n)
	if isNull(n) {
		return false, nil
	}
	if n.Kind != yaml.SequenceNode {
		return false, mismatch("a list", n)
	}
	for i, e := range n.Content {
		if isNull(e) {
			e = nil
		}
		if err := each(e); err != nil {
			return false, fmt.Errorf("[%d]: %w", i, err)
		}
	}
	return true, nil
}

// has is whether an object has the property, null or not.
func has(n *yaml.Node, name string) bool {
	n = resolved(n)
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k := resolved(n.Content[i]); k.Kind == yaml.ScalarNode && k.Value == name {
			return true
		}
	}
	return false
}
