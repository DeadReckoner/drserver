package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
)

//go:embed templates/*.html
var templatesFS embed.FS

// renderers is the per-kind parsed template registry, built at init.
// Each entry is the kind's template file + the shared _layout.html parsed
// into one *template.Template so the kind's "body" and "subject" defines
// resolve against the layout's structure.
var renderers = mustBuildRenderers()

// Vars is the union of fields all templates may reference. Fields not used
// by a given template are simply ignored at render time. Keeping a single
// struct (rather than per-kind types) trades a bit of type-safety for much
// simpler call sites.
type Vars struct {
	FirstName             string
	Email                 string
	TrialEndsAt           string
	ProductName           string
	NextRenewalDate       string
	Price                 string
	AccessUntil           string
	ResubscribeURL        string
	ManageSubscriptionURL string
}

func mustBuildRenderers() map[EmailKind]*template.Template {
	layoutBytes, err := templatesFS.ReadFile("templates/_layout.html")
	if err != nil {
		panic(fmt.Sprintf("email: read _layout.html: %v", err))
	}

	out := make(map[EmailKind]*template.Template, len(AllKinds))
	for _, k := range AllKinds {
		t := template.New(string(k))
		if _, err := t.Parse(string(layoutBytes)); err != nil {
			panic(fmt.Sprintf("email: parse layout for %s: %v", k, err))
		}
		kindBytes, err := templatesFS.ReadFile("templates/" + k.TemplateFile())
		if err != nil {
			panic(fmt.Sprintf("email: read %s: %v", k.TemplateFile(), err))
		}
		if _, err := t.Parse(string(kindBytes)); err != nil {
			panic(fmt.Sprintf("email: parse %s: %v", k.TemplateFile(), err))
		}
		out[k] = t
	}
	return out
}

// Render evaluates a kind's template with vars and returns (subject, html).
// Subject is sourced from the {{define "subject"}} block; html is the full
// rendered document including the layout chrome.
func Render(kind EmailKind, vars Vars) (subject, body string, err error) {
	t, ok := renderers[kind]
	if !ok {
		return "", "", fmt.Errorf("email: unknown kind %q", kind)
	}

	var subjectBuf bytes.Buffer
	if err := t.ExecuteTemplate(&subjectBuf, "subject", vars); err != nil {
		return "", "", fmt.Errorf("render subject: %w", err)
	}

	var htmlBuf bytes.Buffer
	if err := t.Execute(&htmlBuf, vars); err != nil {
		return "", "", fmt.Errorf("render body: %w", err)
	}

	subject = string(bytes.TrimSpace(subjectBuf.Bytes()))
	if subject == "" {
		subject = kind.SubjectFallback()
	}
	body = htmlBuf.String()
	return subject, body, nil
}
