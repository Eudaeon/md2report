package markdown

import (
	"regexp"
	"sort"
	"strings"
)

// Meta holds what the front matter tells md2report: the cover-page fields, the
// author the report is filed under, and the reference that names the incident.
type Meta struct {
	Title     string
	Date      string
	Type      string
	Author    string
	Reference string
	Vars      map[string]string // every front-matter key, cover fields included
}

// varRef spots variable references: {{name}}. The name may hold letters from any
// alphabet, so that {{prénom}} and {{société}} are variables like any other.
var varRef = regexp.MustCompile(`\{\{\s*([\p{L}\p{N}_.-]+)\s*\}\}`)

// parseFrontMatter detaches the minimal YAML front matter from the document it
// precedes, expands {{variables}} within the front matter itself, and returns
// the body untouched. The body is expanded later, once it has been cut into
// blocks, so that a value can never open a block of its own.
//
// A variable may use another at any depth, whatever the declaration order. A
// name still written {{like this}} at the end is unknown or circular; it stays
// visible in the document and the caller hears about it.
//
// The reference names the incident, and with it the file the report is written
// to. The front matter declares it outright, and it expands like every other
// value, so one may be assembled from variables of its own.
//
// Without front matter the body is the whole document and Meta stays empty.
//
// The resolver it returns is what the body is expanded through afterwards: the
// front matter is the whole of what a variable can mean, so everything that
// depends on it is settled here, once.
func parseFrontMatter(src string) (meta Meta, body string, res *resolver) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	meta.Vars = map[string]string{}

	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return meta, src, newResolver(meta.Vars)
	}

	i := 1
	for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
		if k, v, ok := strings.Cut(lines[i], ":"); ok {
			meta.Vars[strings.ToLower(strings.Trim(k, "\ufeff \t"))] = unquote(strings.TrimSpace(v))
		}
		i++
	}
	i++ // closing line

	res = newResolver(meta.Vars)
	// The five keys md2report gives a meaning of its own. Every one is spelled
	// in English, as are the markers a template carries; what an analyst calls
	// their own variables is their business.
	meta.Reference = meta.Vars["reference"]
	meta.Title = meta.Vars["title"]
	meta.Date = meta.Vars["date"]
	meta.Type = meta.Vars["type"]
	meta.Author = meta.Vars["author"]

	return meta, strings.Join(lines[i:], "\n"), res
}

// resolver expands {{variables}} against one front matter. It is built once,
// from the front matter it has just expanded into itself, and it alone knows
// which names lead back to themselves: a caller expands a string and asks
// afterwards what was left written as it was.
type resolver struct {
	vars   map[string]string
	cyclic map[string]bool
	left   map[string]bool // names no expansion could resolve
}

// newResolver settles the cycles and expands the front matter into itself,
// following a reference into the variable it names as far down as the chain
// goes. Names caught in a cycle expand to nothing at all: every reference to one
// stays written as it is, so what the reader sees is what the front matter said.
func newResolver(vars map[string]string) *resolver {
	r := &resolver{vars: vars, left: map[string]bool{}}
	r.cyclic = r.cyclicVars()
	r.expandAll()
	// A cycle is left as written, so nothing else ever expands it and nothing
	// else ever sees what it quotes. Both are collected here instead.
	for name := range r.cyclic {
		r.left[name] = true
		r.expand(r.vars[name])
	}
	return r
}

// expand replaces {{variables}} with their value. A name that is unknown, or
// caught in a cycle, has no value to stand for it: it stays written as it is,
// and unresolved names it afterwards.
func (r *resolver) expand(s string) string {
	return varRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := strings.ToLower(varRef.FindStringSubmatch(ref)[1])
		if v, ok := r.vars[name]; ok && !r.cyclic[name] {
			return v
		}
		r.left[name] = true
		return ref
	})
}

// unresolved names, sorted and without repeats, every variable left written as
// it was: the unknown ones and those caught in a cycle.
func (r *resolver) unresolved() []string {
	if len(r.left) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.left))
	for n := range r.left {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// expandAll expands {{variables}} inside the variables themselves. A value is
// expanded once and remembered, so a chain three hundred deep costs one pass.
func (r *resolver) expandAll() {
	expanded := map[string]string{}
	var expand func(string) string
	expand = func(val string) string {
		return varRef.ReplaceAllStringFunc(val, func(ref string) string {
			name := strings.ToLower(varRef.FindStringSubmatch(ref)[1])
			v, ok := r.vars[name]
			if !ok || r.cyclic[name] {
				r.left[name] = true
				return ref
			}
			if e, ok := expanded[name]; ok {
				return e
			}
			e := expand(v)
			expanded[name] = e
			return e
		})
	}
	for k, v := range r.vars {
		if _, ok := expanded[k]; !ok && !r.cyclic[k] {
			expanded[k] = expand(v)
		}
	}
	for k, v := range expanded {
		r.vars[k] = v
	}
}

// cyclicVars lists the variables that lead back to themselves, directly or
// through others. Leaving every one of them alone is what makes a cycle read the
// same way whichever name expansion happens to reach first.
func (r *resolver) cyclicVars() map[string]bool {
	const (
		unseen = iota
		following
		settled
	)
	state := map[string]int{}
	cyclic := map[string]bool{}
	var stack []string
	var walk func(string)
	walk = func(name string) {
		switch state[name] {
		case settled:
			return
		case following: // back to a name still being followed: the cycle is here
			for i := len(stack) - 1; i >= 0; i-- {
				cyclic[stack[i]] = true
				if stack[i] == name {
					break
				}
			}
			return
		}
		state[name] = following
		stack = append(stack, name)
		for _, ref := range varNames(r.vars[name]) {
			if _, ok := r.vars[ref]; ok {
				walk(ref)
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = settled
	}
	for k := range r.vars {
		walk(k)
	}
	return cyclic
}

// varNames lists, without duplicates, the variables a text refers to.
func varNames(s string) []string {
	var names []string
	seen := map[string]bool{}
	for _, m := range varRef.FindAllStringSubmatch(s, -1) {
		if name := strings.ToLower(m[1]); !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' && s[len(s)-1] == '"' || s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}
