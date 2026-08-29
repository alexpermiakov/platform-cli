package scaffold

import (
	"fmt"
	"regexp"
	"strings"
)

// ResourceProfiles are the t-shirt sizes exposed by the platform's
// standard-service chart. Teams pick a size; the platform team owns the actual
// cpu/memory behind it, so capacity can be retuned centrally without touching
// a single service.
var ResourceProfiles = []string{"small", "medium", "large"}

// Environments are the deploy targets, ordered dev -> prod.
var Environments = []string{"dev", "staging", "prod"}

// dns1123Label matches a Kubernetes namespace / RFC 1123 label. The service
// name becomes the values filename, the ArgoCD Application name, AND the
// namespace, so it has to be valid as all three.
var dns1123Label = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// semver matches the chart version a service may pin itself to.
var semver = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Service is everything the wizard collects. It is the sole input to rendering.
type Service struct {
	Name         string
	Team         string
	Port         int
	Profile      string
	Envs         []string
	Ingress      bool
	IngressPath  string
	ImageTag     string
	ChartVersion string
	RepoURL      string
	PlatformRepo string
	Region       string
}

// Defaults fills the fields a caller can reasonably not care about. Called
// before validation so flag-driven (non-interactive) runs need only the
// genuinely service-specific answers.
func (s *Service) Defaults() {
	if s.Port == 0 {
		s.Port = 8080
	}
	if s.Profile == "" {
		s.Profile = "small"
	}
	if len(s.Envs) == 0 {
		s.Envs = []string{"dev"}
	}
	if s.ImageTag == "" {
		s.ImageTag = "v0.1.0"
	}
	if s.PlatformRepo == "" {
		s.PlatformRepo = "alexpermiakov/paved-road"
	}
	if s.Region == "" {
		s.Region = "us-west-2"
	}
	if s.Ingress && s.IngressPath == "" {
		s.IngressPath = "/" + s.Name
	}
}

func (s *Service) Validate() error {
	switch {
	case s.Name == "":
		return fmt.Errorf("service name is required")
	case len(s.Name) > 63:
		return fmt.Errorf("service name %q is %d chars; must be 63 or fewer (it becomes a Kubernetes namespace)", s.Name, len(s.Name))
	case !dns1123Label.MatchString(s.Name):
		return fmt.Errorf("service name %q must be lowercase alphanumeric with dashes, e.g. \"payment-processor\" (it becomes a Kubernetes namespace)", s.Name)
	case s.Team == "":
		return fmt.Errorf("team is required: the standard-service chart labels every workload with its owner")
	case s.Port < 1 || s.Port > 65535:
		return fmt.Errorf("port %d is out of range 1-65535", s.Port)
	}

	if !contains(ResourceProfiles, s.Profile) {
		return fmt.Errorf("resource profile %q is not one of %s", s.Profile, strings.Join(ResourceProfiles, ", "))
	}
	for _, env := range s.Envs {
		if !contains(Environments, env) {
			return fmt.Errorf("environment %q is not one of %s", env, strings.Join(Environments, ", "))
		}
	}
	if s.Ingress && !strings.HasPrefix(s.IngressPath, "/") {
		return fmt.Errorf("ingress path %q must start with /", s.IngressPath)
	}
	if s.ChartVersion != "" && !semver.MatchString(s.ChartVersion) {
		return fmt.Errorf("chart version %q must be MAJOR.MINOR.PATCH, e.g. \"2.0.0\"", s.ChartVersion)
	}
	return nil
}

// Image is the value the chart expects: repository:tag, with the ECR registry
// prefix supplied per-environment by ArgoCD's platform-values ConfigMap.
func (s *Service) Image() string {
	return fmt.Sprintf("idp/%s:%s", s.Name, s.ImageTag)
}

// ECRRepository is the repo half of Image, which CI needs on its own.
func (s *Service) ECRRepository() string {
	return "idp/" + s.Name
}

// RolloutStrategy encodes a platform rule rather than asking the developer:
// the chart hard-fails a canary rollout without an ingress, because canary
// traffic splitting is done by the ALB. No ingress means blue/green.
func (s *Service) RolloutStrategy() string {
	if s.Ingress {
		return "canary"
	}
	return "blueGreen"
}

// PlatformRepoName is the repo half of owner/repo, for scoping the GitHub App
// token that CI uses to open its deploy PR.
func (s *Service) PlatformRepoName() string {
	if _, name, ok := strings.Cut(s.PlatformRepo, "/"); ok {
		return name
	}
	return s.PlatformRepo
}

func contains(haystack []string, needle string) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
