package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ---------------------------------------------------------------------------
// Validation options
// ---------------------------------------------------------------------------

type ValidationOption struct {
	ID    string
	Label string
	XSLs  []string
}

var validationOptions = []ValidationOption{
	{
		ID:    "EXTENDED-CTC-FR-UBL",
		Label: "EXTENDED-CTC-FR-UBL V1.3 (complet — deux validations)",
		XSLs: []string{
			"data/ubl/EXTENDED-CTC-FR-UBL-V1.3.0.xsl",
			"data/ubl/BR-FR-Flux2-Schematron-UBL_V1.3.0.xsl",
		},
	},
	{
		ID:    "EXTENDED-CTC-FR-CDAR",
		Label: "EXTENDED-CTC-FR-CDAR V1.3.0",
		XSLs:  []string{"data/cdar/BR-FR-CDV-Schematron-CDAR_V1.3.0.xsl"},
	},
}

// ---------------------------------------------------------------------------
// SVRL structures
// ---------------------------------------------------------------------------

const svrlNS = "http://purl.oclc.org/dsdl/svrl"

type SVRLOutput struct {
	XMLName       xml.Name     `xml:"http://purl.oclc.org/dsdl/svrl schematron-output"`
	FailedAsserts []SVRLAssert `xml:"http://purl.oclc.org/dsdl/svrl failed-assert"`
	Reports       []SVRLAssert `xml:"http://purl.oclc.org/dsdl/svrl successful-report"`
}

type SVRLAssert struct {
	ID       string   `xml:"id,attr"`
	Flag     string   `xml:"flag,attr"`
	Location string   `xml:"location,attr"`
	Test     string   `xml:"test,attr"`
	Text     SVRLText `xml:"http://purl.oclc.org/dsdl/svrl text"`
}

// SVRLText captures mixed content (text + child elements).
type SVRLText struct {
	Inner string `xml:",innerxml"`
}

func (t SVRLText) String() string {
	// Strip any XML tags that might be inside <svrl:text>
	var b strings.Builder
	d := xml.NewDecoder(strings.NewReader("<r>" + t.Inner + "</r>"))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok {
			b.Write(cd)
		}
	}
	return strings.TrimSpace(b.String())
}

// ---------------------------------------------------------------------------
// Validation result
// ---------------------------------------------------------------------------

type Issue struct {
	ID       string
	Flag     string
	Location string
	Text     string
}

type ValidationResult struct {
	Valid          bool
	Errors         []Issue
	Warnings       []Issue
	ValidationName string
	ExecError      string
}

// ---------------------------------------------------------------------------
// Saxon runner
// ---------------------------------------------------------------------------

// buildClasspath returns a classpath string containing saxon jar + all other
// jars in the same directory (needed for xmlresolver and other dependencies).
func buildClasspath() (string, error) {
	saxonJar := ""
	if env := os.Getenv("SAXON_JAR"); env != "" {
		saxonJar = env
	} else {
		for _, p := range []string{"saxon*.jar", "Saxon*.jar", "lib/saxon*.jar"} {
			if m, _ := filepath.Glob(p); len(m) > 0 {
				saxonJar = m[0]
				break
			}
		}
	}
	if saxonJar == "" {
		return "", fmt.Errorf(
			"Saxon JAR introuvable. " +
				"Placez saxon-he-*.jar dans le répertoire courant ou définissez la variable SAXON_JAR.")
	}

	// Collect all JARs in the same directory as saxon (deps like xmlresolver)
	dir := filepath.Dir(saxonJar)
	all, _ := filepath.Glob(filepath.Join(dir, "*.jar"))
	sep := ";"
	return strings.Join(all, sep), nil
}

func runSaxon(xmlData []byte, xslPath string) (string, error) {
	cp, err := buildClasspath()
	if err != nil {
		return "", err
	}

	// Write XML to a temp file
	tmp, err := os.CreateTemp("", "xml-validate-*.xml")
	if err != nil {
		return "", fmt.Errorf("impossible de créer le fichier temporaire : %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(xmlData); err != nil {
		tmp.Close()
		return "", fmt.Errorf("erreur d'écriture du fichier temporaire : %w", err)
	}
	tmp.Close()

	// Convert to absolute paths (Saxon prefers them)
	absXSL, _ := filepath.Abs(xslPath)
	absTmp := tmp.Name()

	// Use -cp instead of -jar so that dependency JARs (xmlresolver, etc.) are included
	cmd := exec.Command("java",
		"-cp", cp,
		"net.sf.saxon.Transform",
		"-s:"+absTmp,
		"-xsl:"+absXSL,
		"-warnings:silent",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Saxon exits 0 on success (even with failed asserts in SVRL),
	// non-zero only on fatal transformation errors.
	runErr := cmd.Run()
	output := stdout.String()

	if output == "" && runErr != nil {
		return "", fmt.Errorf("Saxon error: %s", strings.TrimSpace(stderr.String()))
	}
	return output, nil
}

// ---------------------------------------------------------------------------
// SVRL parser
// ---------------------------------------------------------------------------

func parseSVRL(svrlOutput string) ([]Issue, []Issue, error) {
	var doc SVRLOutput
	if err := xml.Unmarshal([]byte(svrlOutput), &doc); err != nil {
		return nil, nil, fmt.Errorf("erreur d'analyse SVRL : %w", err)
	}

	var errors, warnings []Issue
	for _, fa := range doc.FailedAsserts {
		issue := Issue{
			ID:       fa.ID,
			Flag:     fa.Flag,
			Location: fa.Location,
			Text:     fa.Text.String(),
		}
		if strings.EqualFold(fa.Flag, "warning") {
			warnings = append(warnings, issue)
		} else {
			errors = append(errors, issue)
		}
	}
	// successful-report with flag="warning" are warnings too
	for _, r := range doc.Reports {
		if strings.EqualFold(r.Flag, "warning") {
			warnings = append(warnings, Issue{
				ID:       r.ID,
				Flag:     r.Flag,
				Location: r.Location,
				Text:     r.Text.String(),
			})
		}
	}
	return errors, warnings, nil
}

// ---------------------------------------------------------------------------
// HTTP handler
// ---------------------------------------------------------------------------

type PageData struct {
	Options    []ValidationOption
	SelectedID string
	FileName   string
	Result     *ValidationResult
}

func handleIndex(tmpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Options:    validationOptions,
			SelectedID: validationOptions[0].ID,
		}

		if r.Method == http.MethodPost {
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				http.Error(w, "Fichier trop volumineux (max 10 Mo)", http.StatusBadRequest)
				return
			}

			file, header, err := r.FormFile("file")
			if err != nil {
				http.Error(w, "Erreur de lecture du fichier", http.StatusBadRequest)
				return
			}
			defer file.Close()

			xmlData, err := io.ReadAll(file)
			if err != nil {
				http.Error(w, "Erreur de lecture du contenu", http.StatusBadRequest)
				return
			}

			validationID := r.FormValue("validation")
			data.SelectedID = validationID
			data.FileName = header.Filename

			result := &ValidationResult{ValidationName: validationID}

			var opt *ValidationOption
			for i := range validationOptions {
				if validationOptions[i].ID == validationID {
					opt = &validationOptions[i]
					break
				}
			}

			if opt == nil {
				result.ExecError = "Type de validation inconnu : " + validationID
			} else {
				for _, xsl := range opt.XSLs {
					svrl, runErr := runSaxon(xmlData, xsl)
					if runErr != nil {
						result.ExecError = runErr.Error()
						break
					}
					errs, warns, parseErr := parseSVRL(svrl)
					if parseErr != nil {
						result.ExecError = parseErr.Error()
						break
					}
					result.Errors = append(result.Errors, errs...)
					result.Warnings = append(result.Warnings, warns...)
				}
				if result.ExecError == "" {
					result.Valid = len(result.Errors) == 0
				}
			}

			data.Result = result
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	tmpl := template.Must(template.New("page").Parse(htmlPage))
	http.HandleFunc("/", handleIndex(tmpl))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port
	fmt.Printf("Serveur démarré sur http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Erreur serveur : %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// HTML template
// ---------------------------------------------------------------------------

const htmlPage = `<!DOCTYPE html>
<html lang="fr">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Validation XML – AFNOR Schematron</title>
<style>
  *, *::before, *::after { box-sizing: border-box; }
  body { font-family: Segoe UI, Arial, sans-serif; margin: 0; background: #f0f2f5; color: #222; }
  header { background: #003d80; color: #fff; padding: 18px 32px; }
  header h1 { margin: 0; font-size: 1.3rem; letter-spacing: .5px; }
  main { max-width: 860px; margin: 32px auto; padding: 0 16px; }

  /* card */
  .card { background: #fff; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,.1); padding: 28px 32px; margin-bottom: 24px; }

  /* form */
  .field { margin-bottom: 18px; }
  .field label { display: block; font-weight: 600; margin-bottom: 6px; font-size: .9rem; color: #444; }
  .field input[type=file], .field select {
    width: 100%; padding: 9px 12px; border: 1px solid #ccd; border-radius: 5px;
    font-size: .95rem; background: #fafafa;
  }
  .btn {
    display: inline-block; background: #003d80; color: #fff;
    padding: 10px 28px; border: none; border-radius: 5px;
    font-size: .95rem; cursor: pointer; letter-spacing: .3px;
  }
  .btn:hover { background: #00306a; }

  /* result banner */
  .banner { display: flex; align-items: center; gap: 12px; margin-bottom: 16px; font-size: 1.1rem; font-weight: 700; }
  .banner.ok  { color: #1a7a35; }
  .banner.err { color: #c0392b; }
  .banner svg { flex-shrink: 0; }
  .meta { font-size: .82rem; color: #666; margin-bottom: 16px; }
  .meta strong { color: #333; }

  /* issues */
  .section-hd { font-weight: 700; font-size: .85rem; text-transform: uppercase; letter-spacing: .5px; margin: 18px 0 8px; color: #555; }
  .issue { border-radius: 5px; padding: 10px 14px; margin-bottom: 8px; font-size: .88rem; line-height: 1.5; }
  .issue-fatal   { background: #fdf1f1; border-left: 4px solid #e74c3c; }
  .issue-warning { background: #fffbf0; border-left: 4px solid #f1c40f; }
  .issue-id  { font-weight: 700; font-size: .8rem; color: #c0392b; margin-bottom: 3px; }
  .issue-id.warn { color: #7d6608; }
  .issue-loc { font-family: monospace; font-size: .75rem; color: #999; margin-top: 4px; }

  /* exec error */
  .exec-err { background: #fdf1f1; border: 1px solid #f5c6cb; border-radius: 5px; padding: 14px; color: #721c24; font-size: .88rem; white-space: pre-wrap; }
</style>
</head>
<body>
<header><h1>Validation XML – AFNOR Schematron UBL</h1></header>
<main>

  <div class="card">
    <form method="POST" enctype="multipart/form-data">
      <div class="field">
        <label for="f">Fichier XML</label>
        <input type="file" id="f" name="file" accept=".xml" required>
      </div>
      <div class="field">
        <label for="v">Type de validation</label>
        <select id="v" name="validation">
          {{range .Options}}
          <option value="{{.ID}}"{{if eq $.SelectedID .ID}} selected{{end}}>{{.Label}}</option>
          {{end}}
        </select>
      </div>
      <button class="btn" type="submit">Valider</button>
    </form>
  </div>

  {{with .Result}}
  <div class="card">

    {{if .ExecError}}
    <div class="exec-err">{{.ExecError}}</div>
    {{else}}

    {{if .Valid}}
    <div class="banner ok">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/><polyline points="9 12 11 14 15 10"/>
      </svg>
      Le fichier est VALIDE
    </div>
    {{else}}
    <div class="banner err">
      <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><circle cx="12" cy="16" r=".5" fill="currentColor"/>
      </svg>
      Le fichier est INVALIDE
    </div>
    {{end}}

    <div class="meta">
      Fichier : <strong>{{$.FileName}}</strong> &nbsp;|&nbsp; Validation : <strong>{{.ValidationName}}</strong>
    </div>

    {{if .Errors}}
    <div class="section-hd">Erreurs fatales ({{len .Errors}})</div>
    {{range .Errors}}
    <div class="issue issue-fatal">
      <div class="issue-id">{{.Flag}} &mdash; {{.ID}}</div>
      <div>{{.Text}}</div>
      {{if .Location}}<div class="issue-loc">{{.Location}}</div>{{end}}
    </div>
    {{end}}
    {{end}}

    {{if .Warnings}}
    <div class="section-hd">Avertissements ({{len .Warnings}})</div>
    {{range .Warnings}}
    <div class="issue issue-warning">
      <div class="issue-id warn">{{.Flag}} &mdash; {{.ID}}</div>
      <div>{{.Text}}</div>
      {{if .Location}}<div class="issue-loc">{{.Location}}</div>{{end}}
    </div>
    {{end}}
    {{end}}

    {{if and .Valid (not .Warnings)}}
    <p style="color:#666;font-size:.9rem;margin:8px 0 0;">Aucun problème détecté.</p>
    {{end}}

    {{end}}{{/* end not ExecError */}}
  </div>
  {{end}}

</main>
</body>
</html>
`
