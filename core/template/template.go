// Package template renders subject and body templates for outgoing
// messages and appends a custom-fields passthrough block listing form
// values not already named in the templates or reserved.
//
// Templates are parsed at construction time; render is hot-path. Parse
// errors surface from [NewRenderer], which the caller propagates as a
// config-validation error rather than a runtime failure.
//
// "Named fields" — the set of form keys that the template author has
// explicitly accounted for — is the union of:
//
//   - the reserved names supplied to NewRenderer (typically: required
//     fields, the email field, the honeypot field name)
//   - every field reference (e.g., `.name`) the parsed template tree
//     actually mentions
//
// Form keys NOT in the named set are appended to the rendered body as a
// sorted "Additional fields:" block (FR13). This keeps operator inboxes
// useful even when the form gains new fields the template hasn't been
// updated for.
package template

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/template"
	"text/template/parse"
)

// Renderer holds parsed subject and body templates and the named-fields
// set used to compute the passthrough block.
type Renderer struct {
	subject     *template.Template
	body        *template.Template
	namedFields map[string]bool
}

// NewRenderer parses subject and body templates.
//
// `subject` is always treated as inline.
//
// `body` is either inline or a file path. If it contains the substring
// `{{` it's treated as inline; otherwise it's a file path read at
// construction time. (Architecture doc Open Q3 — heuristic per spec;
// validation rejects ambiguity at this point.)
//
// `reservedNames` are field names that should NEVER appear in the
// custom-fields passthrough block, regardless of whether the templates
// reference them (typically: required fields + email_field + honeypot).
// Empty strings in the slice are ignored.
func NewRenderer(subject, body string, reservedNames []string) (*Renderer, error) {
	bodySrc, err := loadBodyTemplate(body)
	if err != nil {
		return nil, err
	}

	// missingkey=zero renders missing map keys as the zero value (empty
	// string for strings) instead of the noisy "<no value>" default.
	// Operators don't want literal "<no value>" appearing in their inboxes
	// when an optional form field is absent.
	subjTpl, err := template.New("subject").Option("missingkey=zero").Parse(subject)
	if err != nil {
		return nil, fmt.Errorf("parse subject template: %w", err)
	}
	bodyTpl, err := template.New("body").Option("missingkey=zero").Parse(bodySrc)
	if err != nil {
		return nil, fmt.Errorf("parse body template: %w", err)
	}

	named := make(map[string]bool, len(reservedNames)+8)
	for _, n := range reservedNames {
		if n != "" {
			named[n] = true
		}
	}
	collectFieldNames(subjTpl.Root, named)
	collectFieldNames(bodyTpl.Root, named)

	return &Renderer{
		subject:     subjTpl,
		body:        bodyTpl,
		namedFields: named,
	}, nil
}

// loadBodyTemplate returns the body source. Inline-vs-file detection:
//
//  1. Contains `{{` → inline (clearly templated)
//  2. Else if body resolves to an existing file → read it
//  3. Else if body looks like a path (contains `/`) → error (typo'd path
//     should be loud, not silently reinterpreted as inline)
//  4. Else → inline literal (allows simple no-template bodies like "Thanks")
//
// Architecture doc Open Q3 / spec heuristic, with an extension for the
// no-template-vars case so operators don't have to add a sentinel `{{}}`.
func loadBodyTemplate(body string) (string, error) {
	if body == "" {
		return "", fmt.Errorf("body is empty")
	}
	if strings.Contains(body, "{{") {
		return body, nil
	}
	if info, err := os.Stat(body); err == nil && !info.IsDir() {
		raw, err := os.ReadFile(body)
		if err != nil {
			return "", fmt.Errorf("read body template file %q: %w", body, err)
		}
		return string(raw), nil
	}
	if strings.Contains(body, "/") {
		return "", fmt.Errorf("body looks like a file path %q but no such file exists", body)
	}
	// Treat as inline literal (no template vars, just static text).
	return body, nil
}

// collectFieldNames walks a parsed template tree and records every field
// reference's first identifier (the `.name` in `.name.last` for example).
// Used to identify fields the template "owns" so they're excluded from
// the passthrough block.
//
// Each case checks for typed-nil before recursing because the standard
// "if node == nil" check at the top of the function only catches a
// completely nil interface — a typed nil pointer (e.g., *parse.ListNode
// that is nil) passes that check but blows up on field access. {{if}}
// blocks legitimately have nil ElseList; this is normal, not a bug.
func collectFieldNames(node parse.Node, set map[string]bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, c := range n.Nodes {
			collectFieldNames(c, set)
		}
	case *parse.ActionNode:
		if n == nil {
			return
		}
		collectFieldNames(n.Pipe, set)
	case *parse.PipeNode:
		if n == nil {
			return
		}
		for _, c := range n.Cmds {
			collectFieldNames(c, set)
		}
	case *parse.CommandNode:
		if n == nil {
			return
		}
		for _, arg := range n.Args {
			collectFieldNames(arg, set)
		}
	case *parse.FieldNode:
		if n == nil {
			return
		}
		if len(n.Ident) > 0 {
			set[n.Ident[0]] = true
		}
	case *parse.ChainNode:
		if n == nil {
			return
		}
		collectFieldNames(n.Node, set)
	case *parse.IfNode:
		if n == nil {
			return
		}
		collectFieldNames(n.Pipe, set)
		collectFieldNames(n.List, set)
		collectFieldNames(n.ElseList, set)
	case *parse.RangeNode:
		if n == nil {
			return
		}
		collectFieldNames(n.Pipe, set)
		collectFieldNames(n.List, set)
		collectFieldNames(n.ElseList, set)
	case *parse.WithNode:
		if n == nil {
			return
		}
		collectFieldNames(n.Pipe, set)
		collectFieldNames(n.List, set)
		collectFieldNames(n.ElseList, set)
	}
}

// RenderSubject executes the subject template against form values.
// Multi-valued fields are joined with ", " before rendering. Missing
// fields render as empty strings (Go template default behavior;
// architecture doc Open Q5 of the prior spec).
func (r *Renderer) RenderSubject(form map[string][]string) (string, error) {
	data := flattenForm(form)
	var buf bytes.Buffer
	if err := r.subject.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render subject: %w", err)
	}
	return buf.String(), nil
}

// RenderBody executes the body template and, if any unnamed form fields
// are present, appends a sorted "Additional fields:" block.
//
// Block format:
//
//	[rendered body]
//
//	Additional fields:
//	  alpha: ...
//	  bravo: ...
func (r *Renderer) RenderBody(form map[string][]string) (string, error) {
	data := flattenForm(form)
	var buf bytes.Buffer
	if err := r.body.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render body: %w", err)
	}

	extras := r.extraFields(form)
	if len(extras) == 0 {
		return buf.String(), nil
	}

	keys := make([]string, 0, len(extras))
	for k := range extras {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := buf.String()
	// Ensure exactly one trailing newline before the block.
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\nAdditional fields:\n"
	for _, k := range keys {
		out += fmt.Sprintf("  %s: %s\n", k, extras[k])
	}
	return out, nil
}

// extraFields returns form entries whose keys are not in the named set.
// Multi-valued fields are joined with ", " for display.
func (r *Renderer) extraFields(form map[string][]string) map[string]string {
	out := map[string]string{}
	for k, v := range form {
		if r.namedFields[k] {
			continue
		}
		if len(v) == 0 {
			continue
		}
		joined := strings.Join(v, ", ")
		if strings.TrimSpace(joined) == "" {
			continue
		}
		out[k] = joined
	}
	return out
}

// flattenForm produces template-friendly map[string]string from
// url.Values-shaped input. Multi-valued fields are joined with ", "
// for natural display in subject/body text.
func flattenForm(form map[string][]string) map[string]string {
	out := make(map[string]string, len(form))
	for k, v := range form {
		if len(v) == 0 {
			continue
		}
		if len(v) == 1 {
			out[k] = v[0]
		} else {
			out[k] = strings.Join(v, ", ")
		}
	}
	return out
}
