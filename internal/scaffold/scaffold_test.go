package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func newService(mutate func(*Service)) *Service {
	s := &Service{Name: "payment-processor", Team: "payments"}
	if mutate != nil {
		mutate(s)
	}
	s.Defaults()
	return s
}

// valuesFor renders one environment's values file and returns it decoded.
// Decoding is the assertion that matters: gopkg.in/yaml.v3 rejects duplicate
// mapping keys, which is how a template that opens the same block twice --
// e.g. a rollout: for blue/green and another for dev analysis -- gets caught.
func valuesFor(t *testing.T, s *Service, env string) map[string]any {
	t.Helper()

	files, err := Render(s)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := filepath.Join("platform-repo", "argocd", "applications", env, "values", s.Name+".yaml")
	for _, f := range files {
		if f.Path != want {
			continue
		}
		var out map[string]any
		if err := yaml.Unmarshal(f.Content, &out); err != nil {
			t.Fatalf("%s is not valid YAML: %v\n---\n%s", f.Path, err, f.Content)
		}
		return out
	}
	t.Fatalf("no values file rendered at %s", want)
	return nil
}

func TestValuesAreValidYAML(t *testing.T) {
	cases := []struct {
		name string
		svc  *Service
		env  string
	}{
		{"ingress dev", newService(func(s *Service) { s.Ingress = true; s.Envs = []string{"dev"} }), "dev"},
		{"ingress prod", newService(func(s *Service) { s.Ingress = true; s.Envs = []string{"prod"} }), "prod"},
		// The regression case: no ingress forces a rollout.strategy block, and
		// dev adds rollout.analysis. Both must land in one mapping.
		{"no ingress dev", newService(func(s *Service) { s.Envs = []string{"dev"} }), "dev"},
		{"no ingress prod", newService(func(s *Service) { s.Envs = []string{"prod"} }), "prod"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valuesFor(t, tc.svc, tc.env)
		})
	}
}

func TestNoIngressKeepsBothRolloutKeys(t *testing.T) {
	values := valuesFor(t, newService(func(s *Service) { s.Envs = []string{"dev"} }), "dev")

	rollout, ok := values["rollout"].(map[string]any)
	if !ok {
		t.Fatalf("rollout block missing or not a mapping: %#v", values["rollout"])
	}
	if got := rollout["strategy"]; got != "blueGreen" {
		t.Errorf("rollout.strategy = %v, want blueGreen (canary requires an ingress)", got)
	}
	if _, ok := rollout["analysis"]; !ok {
		t.Error("rollout.analysis missing; the dev analysis override was dropped")
	}
}

func TestIngressServiceOmitsRolloutStrategy(t *testing.T) {
	values := valuesFor(t, newService(func(s *Service) { s.Ingress = true; s.Envs = []string{"prod"} }), "prod")

	if _, ok := values["rollout"]; ok {
		t.Error("rollout block should be absent so the service inherits the chart's canary default")
	}
	ingress, ok := values["ingress"].(map[string]any)
	if !ok {
		t.Fatalf("ingress block missing: %#v", values["ingress"])
	}
	if ingress["enabled"] != true {
		t.Errorf("ingress.enabled = %v, want true", ingress["enabled"])
	}
}

// The ApplicationSet derives the Application name and namespace from the
// filename while the workload name comes from app.name. If they ever diverge
// you get an Application and a namespace named one thing deploying a workload
// named another.
func TestFilenameMatchesAppName(t *testing.T) {
	s := newService(func(s *Service) { s.Envs = []string{"dev", "staging", "prod"} })

	files, err := Render(s)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var checked int
	for _, f := range files {
		if !strings.Contains(f.Path, "argocd/applications/") {
			continue
		}
		var values struct {
			App struct {
				Name string `yaml:"name"`
			} `yaml:"app"`
		}
		if err := yaml.Unmarshal(f.Content, &values); err != nil {
			t.Fatalf("%s: %v", f.Path, err)
		}
		base := strings.TrimSuffix(filepath.Base(f.Path), ".yaml")
		if base != values.App.Name {
			t.Errorf("%s: filename %q != app.name %q", f.Path, base, values.App.Name)
		}
		checked++
	}
	if checked != 3 {
		t.Errorf("checked %d values files, want 3", checked)
	}
}

func TestRenderProducesOneValuesFilePerEnv(t *testing.T) {
	s := newService(func(s *Service) { s.Envs = []string{"dev", "prod"} })

	files, err := Render(s)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	got := make(map[string]bool, len(files))
	for _, f := range files {
		got[f.Path] = true
	}
	want := []string{
		"platform-repo/argocd/applications/dev/values/payment-processor.yaml",
		"platform-repo/argocd/applications/prod/values/payment-processor.yaml",
		"service-repo/.github/workflows/ci.yaml",
		"service-repo/README.md",
	}
	if len(files) != len(want) {
		t.Errorf("rendered %d files, want %d: %v", len(files), len(want), got)
	}
	for _, w := range want {
		if !got[filepath.FromSlash(w)] {
			t.Errorf("missing %s", w)
		}
	}
}

// No ArgoCD Application manifest: the ApplicationSet generates it. Emitting one
// here would create a second owner for the same namespace.
func TestRenderNeverEmitsAnApplicationManifest(t *testing.T) {
	files, err := Render(newService(nil))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "application.yaml") || strings.Contains(f.Path, "applicationset") {
			t.Errorf("unexpected ArgoCD Application manifest at %s", f.Path)
		}
		if strings.Contains(string(f.Content), "kind: Application") {
			t.Errorf("%s declares an ArgoCD Application", f.Path)
		}
	}
}

func TestCIWorkflowIsValidYAML(t *testing.T) {
	files, err := Render(newService(nil))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Path, "ci.yaml") {
			continue
		}
		var wf map[string]any
		if err := yaml.Unmarshal(f.Content, &wf); err != nil {
			t.Fatalf("ci.yaml is not valid YAML: %v", err)
		}
		jobs, ok := wf["jobs"].(map[string]any)
		if !ok {
			t.Fatal("ci.yaml has no jobs")
		}
		for _, want := range []string{"quality-gate", "build", "deploy"} {
			if _, ok := jobs[want]; !ok {
				t.Errorf("ci.yaml missing job %q", want)
			}
		}
		// GitHub App tokens, never a long-lived PAT.
		if strings.Contains(string(f.Content), "PLATFORM_REPO_TOKEN") {
			t.Error("ci.yaml uses a PAT; it should mint a GitHub App token")
		}
		return
	}
	t.Fatal("no ci.yaml rendered")
}

func TestWriteRefusesToClobber(t *testing.T) {
	dir := t.TempDir()
	files, err := Render(newService(nil))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if err := Write(dir, files, false); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if err := Write(dir, files, false); err == nil {
		t.Fatal("second Write should have refused to overwrite")
	}
	if err := Write(dir, files, true); err != nil {
		t.Fatalf("Write with force: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, files[0].Path)); err != nil {
		t.Errorf("expected %s to exist: %v", files[0].Path, err)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Service)
		wantErr string
	}{
		{"valid", nil, ""},
		{"uppercase name", func(s *Service) { s.Name = "Payment-Processor" }, "lowercase"},
		{"underscore name", func(s *Service) { s.Name = "payment_processor" }, "lowercase"},
		{"trailing dash", func(s *Service) { s.Name = "payments-" }, "lowercase"},
		{"empty name", func(s *Service) { s.Name = "" }, "required"},
		{"name too long", func(s *Service) { s.Name = strings.Repeat("a", 64) }, "63 or fewer"},
		{"missing team", func(s *Service) { s.Team = "" }, "team is required"},
		{"bad profile", func(s *Service) { s.Profile = "extra-large" }, "resource profile"},
		{"bad env", func(s *Service) { s.Envs = []string{"qa"} }, "environment"},
		{"port too high", func(s *Service) { s.Port = 70000 }, "out of range"},
		{"relative ingress path", func(s *Service) { s.Ingress = true; s.IngressPath = "payments" }, "must start with /"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{Name: "payment-processor", Team: "payments"}
			if tc.mutate != nil {
				tc.mutate(s)
			}
			s.Defaults()

			err := s.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected an error containing %q", tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestDefaultsDeriveIngressPathFromName(t *testing.T) {
	s := newService(func(s *Service) { s.Ingress = true })
	if s.IngressPath != "/payment-processor" {
		t.Errorf("IngressPath = %q, want /payment-processor", s.IngressPath)
	}

	// An explicit path wins.
	custom := newService(func(s *Service) { s.Ingress = true; s.IngressPath = "/pay" })
	if custom.IngressPath != "/pay" {
		t.Errorf("IngressPath = %q, want /pay", custom.IngressPath)
	}
}

func TestRolloutStrategyFollowsIngress(t *testing.T) {
	if got := newService(func(s *Service) { s.Ingress = true }).RolloutStrategy(); got != "canary" {
		t.Errorf("with ingress: got %q, want canary", got)
	}
	if got := newService(nil).RolloutStrategy(); got != "blueGreen" {
		t.Errorf("without ingress: got %q, want blueGreen", got)
	}
}

func TestPlatformRepoName(t *testing.T) {
	s := newService(func(s *Service) { s.PlatformRepo = "alexpermiakov/paved-road" })
	if got := s.PlatformRepoName(); got != "paved-road" {
		t.Errorf("PlatformRepoName() = %q, want paved-road", got)
	}
	bare := newService(func(s *Service) { s.PlatformRepo = "paved-road" })
	if got := bare.PlatformRepoName(); got != "paved-road" {
		t.Errorf("PlatformRepoName() = %q, want paved-road", got)
	}
}

func TestChartVersionPinIsOptional(t *testing.T) {
	// Unpinned: the service tracks whatever default the ApplicationSet carries,
	// so the key must be absent rather than empty.
	values := valuesFor(t, newService(func(s *Service) { s.Envs = []string{"dev"} }), "dev")
	if _, ok := values["chartVersion"]; ok {
		t.Error("chartVersion should be absent unless explicitly pinned")
	}

	pinned := valuesFor(t, newService(func(s *Service) {
		s.Envs = []string{"dev"}
		s.ChartVersion = "2.0.0"
	}), "dev")
	if got := pinned["chartVersion"]; got != "2.0.0" {
		t.Errorf("chartVersion = %v, want 2.0.0", got)
	}
}

func TestChartVersionMustBeSemver(t *testing.T) {
	for _, bad := range []string{"2.0", "v2.0.0", "latest", "2.0.0-rc1"} {
		s := newService(func(s *Service) { s.ChartVersion = bad })
		if err := s.Validate(); err == nil {
			t.Errorf("chart version %q should have been rejected", bad)
		}
	}
	s := newService(func(s *Service) { s.ChartVersion = "2.0.0" })
	if err := s.Validate(); err != nil {
		t.Errorf("2.0.0 should be valid: %v", err)
	}
}

// app.team is required by the chart's values.schema.json, so a generated file
// without one fails at render time rather than at review time.
func TestGeneratedValuesAlwaysCarryATeam(t *testing.T) {
	values := valuesFor(t, newService(func(s *Service) { s.Envs = []string{"dev"} }), "dev")
	app, ok := values["app"].(map[string]any)
	if !ok {
		t.Fatalf("app block missing: %#v", values["app"])
	}
	if app["team"] != "payments" {
		t.Errorf("app.team = %v, want payments", app["team"])
	}
}

// replicaCount was removed from the chart in 2.0.0 and is now rejected as an
// unknown key by values.schema.json.
func TestGeneratedValuesNeverEmitReplicaCount(t *testing.T) {
	files, err := Render(newService(func(s *Service) { s.Envs = []string{"dev", "staging", "prod"} }))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(string(f.Content), "replicaCount") {
			t.Errorf("%s sets replicaCount, which the chart rejects", f.Path)
		}
	}
}
