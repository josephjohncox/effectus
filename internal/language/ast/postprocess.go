package ast

import "strconv"

func unquoteString(value string) string {
	if value == "" {
		return value
	}
	unquoted, err := strconv.Unquote(value)
	if err != nil {
		return value
	}
	return unquoted
}

// PostProcess normalizes parsed rule names.
func (r *Rule) PostProcess() {
	r.Name = unquoteString(r.Name)
}

// PostProcess normalizes parsed flow names.
func (f *Flow) PostProcess() {
	f.Name = unquoteString(f.Name)
}

// PostProcess normalizes string literals.
func (l *Literal) PostProcess() {
	if l == nil || l.String == nil {
		return
	}
	value := unquoteString(*l.String)
	l.String = &value
}
