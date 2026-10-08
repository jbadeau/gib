package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// expandArgFiles replaces each @file argument with the arguments the
// file holds, as picocli does: split at whitespace, quotes grouping and
// dropped, # starting a comment. An @file that does not exist stays as
// it is; @@ escapes a literal @.
func expandArgFiles(args []string) ([]string, error) {
	return expand(args, map[string]bool{})
}

func expand(args []string, seen map[string]bool) ([]string, error) {
	var out []string
	for _, a := range args {
		if strings.HasPrefix(a, "@@") {
			out = append(out, a[1:])
			continue
		}
		if !strings.HasPrefix(a, "@") || len(a) == 1 {
			out = append(out, a)
			continue
		}
		file := a[1:]
		data, err := os.ReadFile(file)
		if err != nil {
			out = append(out, a)
			continue
		}
		if seen[file] {
			return nil, fmt.Errorf("@-file %s includes itself", file)
		}
		seen[file] = true
		more, err := expand(tokenize(string(data)), seen)
		if err != nil {
			return nil, err
		}
		delete(seen, file)
		out = append(out, more...)
	}
	return out, nil
}

func tokenize(s string) []string {
	var out []string
	var b strings.Builder
	in, quote, comment := false, rune(0), false
	for _, r := range s {
		switch {
		case comment:
			if r == '\n' {
				comment = false
			}
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, in = r, true
		case r == '#' && !in:
			comment = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f':
			if in {
				out = append(out, b.String())
				b.Reset()
				in = false
			}
		default:
			b.WriteRune(r)
			in = true
		}
	}
	if in {
		out = append(out, b.String())
	}
	return out
}

// attachOptionalValues gives each option that takes a value or none its
// value, as picocli's arity 0..1 does: a password option takes the next
// argument unless that is an option, --http-trace only one of its
// levels.
func attachOptionalValues(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i:]...)
		}
		if i+1 < len(args) {
			next := args[i+1]
			switch a {
			case "--password", "--to-password", "--from-password":
				if !strings.HasPrefix(next, "-") {
					out = append(out, a+"="+next)
					i++
					continue
				}
			case "--http-trace":
				if next == "off" || next == "config" || next == "all" {
					out = append(out, a+"="+next)
					i++
					continue
				}
			}
		}
		out = append(out, a)
	}
	return out
}

// flagError is a flag pflag refuses, worded as picocli words it.
func flagError(cmd *cobra.Command, err error) error {
	var (
		notExist *pflag.NotExistError
		required *pflag.ValueRequiredError
		invalid  *pflag.InvalidValueError
	)
	switch {
	case errors.As(err, &notExist):
		name := notExist.GetSpecifiedName()
		if s := notExist.GetSpecifiedShortnames(); s != "" {
			return usagef(cmd, "Unknown option: '-%s'", s[:1])
		}
		return usagef(cmd, "Unknown option: '--%s'", name)
	case errors.As(err, &required):
		f := required.GetFlag()
		return usagef(cmd, "Missing required parameter for option '--%s' (%s)", f.Name, label(f))
	case errors.As(err, &invalid):
		return usagef(cmd, "%s", invalid.Unwrap())
	}
	return usagef(cmd, "%s", err)
}

// label is a flag's parameter label, as picocli prints it.
func label(f *pflag.Flag) string {
	if l, ok := f.Annotations["label"]; ok {
		return l[0]
	}
	return "<" + f.Name + ">"
}

// once is a single-valued option, refused when given twice as picocli
// refuses it.
type once struct {
	name, label string
	value       *string
	set         bool
}

func (o *once) String() string { return *o.value }
func (o *once) Type() string   { return "string" }
func (o *once) Set(v string) error {
	if o.set {
		return fmt.Errorf("option '--%s' (%s) should be specified only once", o.name, o.label)
	}
	o.set = true
	*o.value = v
	return nil
}

// choice is an option that takes one of a fixed set of values.
type choice struct {
	once
	choices []string
}

func (c *choice) Set(v string) error {
	for _, ok := range c.choices {
		if v == ok {
			return c.once.Set(v)
		}
	}
	return fmt.Errorf("Invalid value for option '--%s': expected one of [%s] (case-sensitive) but was '%s'", //nolint:staticcheck // picocli's message
		c.name, strings.Join(c.choices, ", "), v)
}

// keyValues is a repeatable name=value option, each value kept whole as
// picocli keeps a map option's.
type keyValues struct {
	name, label string
	m           map[string]string
}

func (k *keyValues) String() string { return "" }
func (k *keyValues) Type() string   { return "stringToString" }
func (k *keyValues) Set(v string) error {
	key, value, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("Value for option option '--%s' (%s) should be in KEY=VALUE format but was %s", k.name, k.label, v) //nolint:staticcheck // picocli's message
	}
	k.m[key] = value
	return nil
}

// list is a repeatable option whose values are also split at commas.
type list struct{ values *[]string }

func (l *list) String() string { return strings.Join(*l.values, ",") }
func (l *list) Type() string   { return "strings" }
func (l *list) Set(v string) error {
	*l.values = append(*l.values, strings.Split(v, ",")...)
	return nil
}
