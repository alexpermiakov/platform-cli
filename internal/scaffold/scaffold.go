package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

// File is one rendered artifact and the path it belongs at, relative to the
// output directory.
type File struct {
	Path    string
	Content []byte
}

// valuesCtx is the render context for a per-environment values file. The
// rollout flags are computed here rather than in the template so the template
// never has to reason about whether the block it is opening is empty.
type valuesCtx struct {
	Service     *Service
	Env         string
	HasRollout  bool
	DevAnalysis bool
}

// Render produces every file for a service without touching disk, so callers
// can preview (--dry-run) and write through the same code path.
func Render(s *Service) ([]File, error) {
	var files []File

	for _, env := range s.Envs {
		devAnalysis := env == "dev"
		ctx := valuesCtx{
			Service:     s,
			Env:         env,
			DevAnalysis: devAnalysis,
			HasRollout:  devAnalysis || !s.Ingress,
		}
		content, err := render("values.yaml.tmpl", ctx)
		if err != nil {
			return nil, fmt.Errorf("rendering %s values: %w", env, err)
		}
		files = append(files, File{
			// The basename must equal s.Name: the ApplicationSet derives the
			// Application name and the namespace from the filename, while the
			// workload name comes from app.name inside the file.
			Path:    filepath.Join("platform-repo", "argocd", "applications", env, "values", s.Name+".yaml"),
			Content: content,
		})
	}

	ci, err := render("ci.yaml.tmpl", s)
	if err != nil {
		return nil, fmt.Errorf("rendering ci workflow: %w", err)
	}
	files = append(files, File{
		Path:    filepath.Join("service-repo", ".github", "workflows", "ci.yaml"),
		Content: ci,
	})

	readme, err := render("README.md.tmpl", s)
	if err != nil {
		return nil, fmt.Errorf("rendering readme: %w", err)
	}
	files = append(files, File{
		Path:    filepath.Join("service-repo", "README.md"),
		Content: readme,
	})

	return files, nil
}

// Write puts rendered files under dir. Without force it refuses to clobber, so
// re-running against a populated directory is safe by default.
func Write(dir string, files []File, force bool) error {
	if !force {
		var existing []string
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(dir, f.Path)); err == nil {
				existing = append(existing, f.Path)
			}
		}
		if len(existing) > 0 {
			return fmt.Errorf("refusing to overwrite %d existing file(s), starting with %s; pass --force to replace them", len(existing), existing[0])
		}
	}

	for _, f := range files {
		full := filepath.Join(dir, f.Path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, f.Content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", full, err)
		}
	}
	return nil
}

// render executes one embedded template. Delimiters are {% %} because the
// output collides with everything else: Helm and GitHub Actions both use
// {{ }}, and the CI template contains bash [[ ]] conditionals.
func render(name string, data any) ([]byte, error) {
	tmpl, err := template.New(name).
		Delims("{%", "%}").
		Option("missingkey=error").
		ParseFS(templatesFS, "templates/"+name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
