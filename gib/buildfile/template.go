package buildfile

import (
	"fmt"
	"strings"
)

// substitute replaces a build file's template parameters as Jib does,
// reading it through commons-text's StringSubstitutorReader over a
// StringSubstitutor that fails on an undefined variable. The reader
// passes text through until a variable starts, "${" or the escaped
// "$${", reads on to the "}" that balances it, and substitutes that
// stretch alone; a stretch that never balances is substituted with
// the rest of the text.
func substitute(text string, params map[string]string) (string, error) {
	var out strings.Builder
	i := 0
	for i < len(text) {
		j := i
		for j < len(text) && !strings.HasPrefix(text[j:], "${") && !strings.HasPrefix(text[j:], "$${") {
			j++
		}
		out.WriteString(text[i:j])
		if j == len(text) {
			break
		}
		end := balanced(text, j)
		if end < 0 {
			s, err := replace(text[j:], params)
			if err != nil {
				return "", err
			}
			out.WriteString(s)
			break
		}
		// The reader substitutes the stretch with the character after
		// it, which no substitution changes, and then reads on from that
		// character.
		seg := text[j:min(end+1, len(text))]
		s, err := replace(seg, params)
		if err != nil {
			return "", err
		}
		out.WriteString(s[:len(s)-(len(seg)-(end-j))])
		i = end
	}
	return out.String(), nil
}

// balanced is the end of the variable that starts at j, just past the
// "}" that closes it, counting each "${" and "$${" before it as one
// more to close; -1 when the text ends first.
func balanced(s string, j int) int {
	pos := j + 2
	if s[j+1] == '$' {
		pos++
	}
	open := 1
	for pos < len(s) {
		switch {
		case s[pos] == '}':
			open--
			pos++
			if open == 0 {
				return pos
			}
		case strings.HasPrefix(s[pos:], "${"):
			open++
			pos += 2
		case strings.HasPrefix(s[pos:], "$${"):
			open++
			pos += 3
		default:
			pos++
		}
	}
	return -1
}

// replace is StringSubstitutor.replace with "${" and "}" around a
// name, ":-" before a default, "$" escaping, values substituted in
// turn, and no substitution inside a name.
func replace(s string, params map[string]string) (string, error) {
	r := &replacer{b: []byte(s), params: params}
	if _, err := r.substitute(0, len(s)); err != nil {
		return "", err
	}
	return string(r.b), nil
}

type replacer struct {
	b      []byte
	params map[string]string
	prior  []string
}

func (r *replacer) prefixAt(pos, end int) bool {
	return pos+1 < end && r.b[pos] == '$' && r.b[pos+1] == '{'
}

// substitute substitutes in b[offset:offset+length] and gives the
// change in its length.
func (r *replacer) substitute(offset, length int) (int, error) {
	change := 0
	end := offset + length
	pos := offset
	esc := -1
outer:
	for pos < end {
		if !r.prefixAt(pos, end) {
			pos++
			continue
		}
		if pos > offset && r.b[pos-1] == '$' {
			esc = pos - 1
		}
		start := pos
		pos += 2
		for pos < end {
			if r.b[pos] != '}' {
				pos++
				continue
			}
			if esc >= 0 {
				r.b = append(r.b[:esc], r.b[esc+1:]...)
				esc = -1
				change--
				end--
				pos = start + 1
				continue outer
			}
			expr := string(r.b[start+2 : pos])
			pos++
			stop := pos
			name, def, hasDef := expr, "", false
			for i := 0; i < len(expr); i++ {
				if strings.HasPrefix(expr[i:], "${") {
					break
				}
				if strings.HasPrefix(expr[i:], ":-") {
					name, def, hasDef = expr[:i], expr[i+2:], true
					break
				}
			}
			if r.prior == nil {
				r.prior = []string{string(r.b[offset:min(offset+length, len(r.b))])}
			}
			for _, p := range r.prior {
				if p == name {
					return 0, fmt.Errorf("Infinite loop in property interpolation of %s: %s", r.prior[0], strings.Join(r.prior[1:], "->")) //nolint:staticcheck // commons-text's message
				}
			}
			r.prior = append(r.prior, name)
			v, ok := r.params[name]
			if !ok && hasDef {
				v, ok = def, true
			}
			if !ok {
				return 0, fmt.Errorf("Cannot resolve variable '%s' (enableSubstitutionInVariables=false).", name) //nolint:staticcheck // commons-text's message
			}
			r.b = append(r.b[:start], append([]byte(v), r.b[stop:]...)...)
			c, err := r.substitute(start, len(v))
			if err != nil {
				return 0, err
			}
			c += len(v) - (stop - start)
			pos += c
			end += c
			change += c
			r.prior = r.prior[:len(r.prior)-1]
			break
		}
	}
	return change, nil
}
