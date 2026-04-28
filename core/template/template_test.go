package template_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigmccaskill/posthorn/template"
)

// --- Construction ---

func TestNewRenderer_InlineTemplates(t *testing.T) {
	r, err := template.NewRenderer("Subject: {{.name}}", "Hello {{.name}}", nil)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	if r == nil {
		t.Fatal("nil renderer with nil error")
	}
}

func TestNewRenderer_BodyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.tmpl")
	if err := os.WriteFile(path, []byte("Body: {{.message}}"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	r, err := template.NewRenderer("S", path, nil)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	out, err := r.RenderBody(map[string][]string{"message": {"hello"}})
	if err != nil {
		t.Fatalf("RenderBody: %v", err)
	}
	if !strings.Contains(out, "Body: hello") {
		t.Errorf("body = %q", out)
	}
}

func TestNewRenderer_BodyInlineDetection(t *testing.T) {
	// Inline detection: a value containing `{{` is treated as inline,
	// not a file path. So "{{.x}}" should not try to read a file.
	r, err := template.NewRenderer("S", "literal {{.x}} body", nil)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	out, _ := r.RenderBody(map[string][]string{"x": {"value"}})
	if !strings.Contains(out, "literal value body") {
		t.Errorf("body = %q", out)
	}
}

func TestNewRenderer_BodyFileNotFound(t *testing.T) {
	_, err := template.NewRenderer("S", "/no/such/file.tmpl", nil)
	if err == nil {
		t.Fatal("expected error for missing body file")
	}
	if !strings.Contains(err.Error(), "/no/such/file.tmpl") {
		t.Errorf("error should mention the path: %v", err)
	}
}

func TestNewRenderer_SubjectParseError(t *testing.T) {
	// `{{.x` is unclosed action — parse error.
	_, err := template.NewRenderer("Bad: {{.x", "ok body {{.y}}", nil)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "subject") {
		t.Errorf("error should mention subject: %v", err)
	}
}

func TestNewRenderer_BodyParseError(t *testing.T) {
	_, err := template.NewRenderer("S {{.x}}", "Bad: {{.x", nil)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !strings.Contains(err.Error(), "body") {
		t.Errorf("error should mention body: %v", err)
	}
}

func TestNewRenderer_EmptyBody(t *testing.T) {
	_, err := template.NewRenderer("S", "", nil)
	if err == nil {
		t.Fatal("expected error for empty body")
	}
}

// --- Subject rendering ---

func TestRenderSubject_Success(t *testing.T) {
	r, _ := template.NewRenderer("Contact from {{.name}}", "B {{.x}}", nil)
	got, err := r.RenderSubject(map[string][]string{"name": {"craig"}})
	if err != nil {
		t.Fatalf("RenderSubject: %v", err)
	}
	if got != "Contact from craig" {
		t.Errorf("got %q", got)
	}
}

func TestRenderSubject_MissingFieldRendersEmpty(t *testing.T) {
	// FR12 / Go template default: missing field renders as the zero value
	// for its type, which for map lookups is the empty string.
	r, _ := template.NewRenderer("Hi {{.name}}!", "B {{.x}}", nil)
	got, _ := r.RenderSubject(map[string][]string{})
	if got != "Hi !" {
		t.Errorf("got %q, want %q (missing field should render empty)", got, "Hi !")
	}
}

func TestRenderSubject_MultiValueJoinedComma(t *testing.T) {
	r, _ := template.NewRenderer("Tags: {{.tag}}", "B {{.x}}", nil)
	got, _ := r.RenderSubject(map[string][]string{"tag": {"a", "b", "c"}})
	if got != "Tags: a, b, c" {
		t.Errorf("got %q", got)
	}
}

// --- Body rendering ---

func TestRenderBody_Success(t *testing.T) {
	r, _ := template.NewRenderer("S", "From {{.name}}: {{.message}}", nil)
	got, _ := r.RenderBody(map[string][]string{
		"name":    {"craig"},
		"message": {"hello"},
	})
	if got != "From craig: hello" {
		t.Errorf("got %q", got)
	}
}

// --- Custom-fields passthrough ---

func TestRenderBody_NoExtras_NoBlock(t *testing.T) {
	// All form fields are referenced in the template — no passthrough.
	r, _ := template.NewRenderer("S", "{{.name}} {{.message}}", nil)
	got, _ := r.RenderBody(map[string][]string{
		"name":    {"craig"},
		"message": {"hi"},
	})
	if strings.Contains(got, "Additional fields") {
		t.Errorf("body = %q, should not contain Additional fields block", got)
	}
}

func TestRenderBody_WithExtras_BlockAppended(t *testing.T) {
	// `company` and `source` are not referenced in the template; they appear
	// in the passthrough block, sorted.
	r, _ := template.NewRenderer("S", "From {{.name}}", nil)
	got, _ := r.RenderBody(map[string][]string{
		"name":    {"craig"},
		"source":  {"HN"},
		"company": {"Acme"},
	})
	want := "From craig\n\nAdditional fields:\n  company: Acme\n  source: HN\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRenderBody_ExtrasSorted(t *testing.T) {
	// Sort order is alphabetical on key, regardless of map iteration order.
	r, _ := template.NewRenderer("S", "Hi", nil)
	got, _ := r.RenderBody(map[string][]string{
		"zeta":  {"z"},
		"alpha": {"a"},
		"mike":  {"m"},
	})
	idxAlpha := strings.Index(got, "alpha:")
	idxMike := strings.Index(got, "mike:")
	idxZeta := strings.Index(got, "zeta:")
	if idxAlpha < 0 || idxMike < 0 || idxZeta < 0 {
		t.Fatalf("missing keys in body: %q", got)
	}
	if !(idxAlpha < idxMike && idxMike < idxZeta) {
		t.Errorf("not sorted alphabetically: alpha@%d mike@%d zeta@%d", idxAlpha, idxMike, idxZeta)
	}
}

func TestRenderBody_ReservedNamesExcluded(t *testing.T) {
	// `honeypot` and `_csrf` are reserved by config; they should not appear
	// in the passthrough block even though they're not in the template.
	r, _ := template.NewRenderer(
		"S",
		"From {{.name}}",
		[]string{"honeypot", "_csrf"},
	)
	got, _ := r.RenderBody(map[string][]string{
		"name":     {"craig"},
		"honeypot": {""},
		"_csrf":    {"abc123"},
		"company":  {"Acme"},
	})
	if strings.Contains(got, "honeypot") {
		t.Errorf("honeypot leaked into body: %q", got)
	}
	if strings.Contains(got, "_csrf") {
		t.Errorf("_csrf leaked into body: %q", got)
	}
	if !strings.Contains(got, "company: Acme") {
		t.Errorf("company missing from body: %q", got)
	}
}

func TestRenderBody_TemplateReferencedFieldsExcluded(t *testing.T) {
	// `name` is in the template; it should not be re-listed in extras.
	r, _ := template.NewRenderer("S", "Hi {{.name}}", nil)
	got, _ := r.RenderBody(map[string][]string{
		"name":  {"craig"},
		"other": {"value"},
	})
	if strings.Count(got, "name:") > 0 {
		t.Errorf("name appeared as extra (was in template): %q", got)
	}
	if !strings.Contains(got, "other: value") {
		t.Errorf("other field missing: %q", got)
	}
}

func TestRenderBody_EmptyExtraValuesSkipped(t *testing.T) {
	// Extras with empty values shouldn't pollute the block.
	r, _ := template.NewRenderer("S", "Hi", nil)
	got, _ := r.RenderBody(map[string][]string{
		"a": {"value"},
		"b": {""},
		"c": {"   "}, // whitespace-only
	})
	if !strings.Contains(got, "a: value") {
		t.Errorf("missing a: %q", got)
	}
	if strings.Contains(got, "b:") {
		t.Errorf("empty b leaked: %q", got)
	}
	if strings.Contains(got, "c:") {
		t.Errorf("whitespace c leaked: %q", got)
	}
}

func TestRenderBody_TrailingNewlineNormalized(t *testing.T) {
	// Body without trailing newline should still produce the block with
	// proper blank-line separation.
	r, _ := template.NewRenderer("S", "no newline", nil)
	got, _ := r.RenderBody(map[string][]string{"extra": {"v"}})
	if !strings.Contains(got, "no newline\n\nAdditional fields:") {
		t.Errorf("expected blank line before block; got:\n%q", got)
	}
}

func TestRenderBody_TemplateWithNewlineDoesNotDoubleUp(t *testing.T) {
	// Body that already ends in newline shouldn't get an extra one.
	r, _ := template.NewRenderer("S", "with newline\n", nil)
	got, _ := r.RenderBody(map[string][]string{"extra": {"v"}})
	if strings.Contains(got, "\n\n\nAdditional fields:") {
		t.Errorf("triple newline before block; got:\n%q", got)
	}
}

// --- Complex template syntax ---

func TestRender_ConditionalSyntaxParses(t *testing.T) {
	// {{if}} blocks should parse without trouble.
	r, err := template.NewRenderer(
		"S",
		"{{if .priority}}URGENT: {{end}}{{.message}}",
		nil,
	)
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	got, _ := r.RenderBody(map[string][]string{
		"priority": {"high"},
		"message":  {"hello"},
	})
	if !strings.Contains(got, "URGENT: hello") {
		t.Errorf("got %q", got)
	}
}

func TestRender_ConditionalFieldsCountedAsNamed(t *testing.T) {
	// Fields inside {{if}}{{end}} should still register as named — i.e.,
	// not appear in the passthrough block.
	r, _ := template.NewRenderer(
		"S",
		"{{if .priority}}URGENT{{end}}",
		nil,
	)
	got, _ := r.RenderBody(map[string][]string{
		"priority": {"high"},
		"company":  {"Acme"},
	})
	if strings.Contains(got, "priority:") {
		t.Errorf("priority leaked into extras (was in conditional): %q", got)
	}
	if !strings.Contains(got, "company: Acme") {
		t.Errorf("company should be in extras: %q", got)
	}
}
