package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/alexpermiakov/platform-cli/internal/scaffold"
)

type initOptions struct {
	svc            scaffold.Service
	out            string
	dryRun         bool
	force          bool
	nonInteractive bool
}

func newInitCommand() *cobra.Command {
	var opts initOptions

	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Scaffold a new service",
		Args:  cobra.MaximumNArgs(1),
		Example: `  platform init
  platform init payment-processor --team payments --profile medium --ingress
  platform init worker --team payments --non-interactive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				opts.svc.Name = args[0]
			}
			return runInit(&opts)
		},
	}

	f := cmd.Flags()
	f.StringVar(&opts.svc.Team, "team", "", "team that owns the service")
	f.IntVar(&opts.svc.Port, "port", 0, "container port (default 8080)")
	f.StringVar(&opts.svc.Profile, "profile", "", "resource profile: small, medium, or large (default small)")
	f.StringSliceVar(&opts.svc.Envs, "env", nil, "environments to onboard: dev, staging, prod (default dev)")
	f.BoolVar(&opts.svc.Ingress, "ingress", false, "expose the service through the shared ALB")
	f.StringVar(&opts.svc.IngressPath, "ingress-path", "", "ingress path (default /<name>)")
	f.StringVar(&opts.svc.ImageTag, "image-tag", "", "initial image tag (default v0.1.0)")
	f.StringVar(&opts.svc.ChartVersion, "chart-version", "", "pin standard-service to this version (default: track the platform default)")
	f.StringVar(&opts.svc.PlatformRepo, "platform-repo", "", "owner/repo of the platform repository")
	f.StringVar(&opts.svc.Region, "region", "", "AWS region (default us-west-2)")
	f.StringVarP(&opts.out, "out", "o", "out", "parent directory for generated files; each service gets its own subdirectory")
	f.BoolVar(&opts.dryRun, "dry-run", false, "print what would be generated without writing")
	f.BoolVar(&opts.force, "force", false, "overwrite existing files")
	f.BoolVar(&opts.nonInteractive, "non-interactive", false, "skip the wizard and use flags only")

	return cmd
}

func runInit(opts *initOptions) error {
	if !opts.nonInteractive {
		if err := askAll(&opts.svc); err != nil {
			return err
		}
	}

	opts.svc.Defaults()
	if err := opts.svc.Validate(); err != nil {
		return err
	}

	// Each service targets its own repository, so two services scaffolded into
	// one directory would collide on service-repo/. Every scaffold gets its own
	// subdirectory.
	opts.out = filepath.Join(opts.out, opts.svc.Name)

	files, err := scaffold.Render(&opts.svc)
	if err != nil {
		return err
	}

	if opts.dryRun {
		for _, f := range files {
			fmt.Printf("\n===== %s =====\n%s", f.Path, f.Content)
		}
		return nil
	}

	if err := scaffold.Write(opts.out, files, opts.force); err != nil {
		return err
	}

	printNextSteps(&opts.svc, opts.out, files)
	return nil
}

// askAll runs the wizard, seeded with whatever flags already supplied so a
// partially-flagged invocation only prompts for what is missing a value.
func askAll(s *scaffold.Service) error {
	port := ""
	if s.Port != 0 {
		port = strconv.Itoa(s.Port)
	}
	if s.Profile == "" {
		s.Profile = "small"
	}
	if len(s.Envs) == 0 {
		s.Envs = []string{"dev"}
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Service name").
				Description("Becomes the Kubernetes namespace and the ArgoCD Application name.").
				Placeholder("payment-processor").
				Value(&s.Name).
				Validate(func(v string) error {
					probe := scaffold.Service{Name: v, Team: "x", Port: 8080, Profile: "small"}
					probe.Defaults()
					return firstNameError(probe.Validate())
				}),

			huh.NewInput().
				Title("Owning team").
				Description("Labelled onto every pod; required by platform policy.").
				Placeholder("payments").
				Value(&s.Team).
				Validate(func(v string) error {
					if strings.TrimSpace(v) == "" {
						return fmt.Errorf("required")
					}
					return nil
				}),

			huh.NewInput().
				Title("Container port").
				Placeholder("8080").
				Value(&port).
				Validate(func(v string) error {
					if v == "" {
						return nil
					}
					n, err := strconv.Atoi(v)
					if err != nil {
						return fmt.Errorf("must be a number")
					}
					if n < 1 || n > 65535 {
						return fmt.Errorf("must be between 1 and 65535")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Title("Resource profile").
				Description("The platform team owns the cpu/memory behind each size.").
				Options(huh.NewOptions(scaffold.ResourceProfiles...)...).
				Value(&s.Profile),

			huh.NewMultiSelect[string]().
				Title("Environments to onboard").
				Description("Start with dev; add staging and prod once it is running.").
				Options(huh.NewOptions(scaffold.Environments...)...).
				Value(&s.Envs).
				Validate(func(v []string) error {
					if len(v) == 0 {
						return fmt.Errorf("pick at least one")
					}
					return nil
				}),

			huh.NewConfirm().
				Title("Reachable from outside the cluster?").
				Description("No keeps it cluster-internal and switches rollouts to blue/green.").
				Value(&s.Ingress),
		),

		huh.NewGroup(
			huh.NewInput().
				Title("Ingress path").
				Placeholder("/payments").
				Value(&s.IngressPath).
				Validate(func(v string) error {
					if v != "" && !strings.HasPrefix(v, "/") {
						return fmt.Errorf("must start with /")
					}
					return nil
				}),
		).WithHideFunc(func() bool { return !s.Ingress }),
	)

	if err := form.Run(); err != nil {
		return err
	}

	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("port %q is not a number", port)
		}
		s.Port = n
	}
	return nil
}

// firstNameError keeps the inline validator focused on the name field: the
// probe service carries dummy values for everything else, so only a name
// complaint is meaningful here.
func firstNameError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "service name") {
		return err
	}
	return nil
}

func printNextSteps(s *scaffold.Service, out string, files []scaffold.File) {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}

	fmt.Fprintf(os.Stdout, "\nScaffolded %s (team %s) into %s\n\n", s.Name, s.Team, abs)
	for _, f := range files {
		fmt.Printf("  %s\n", f.Path)
	}

	fmt.Printf(`
These go to two different repositories.

  1. platform-repo/  ->  %s
     Open a PR. Merging it onboards the service: the ApplicationSet picks up
     the values file and creates the ArgoCD Application and namespace.

  2. service-repo/   ->  your service's own repository
     Commit alongside your code. CI needs AWS_ROLE_ARN and PLATFORM_APP_ID as
     variables, and PLATFORM_APP_PRIVATE_KEY as a secret.

Before the first deploy, make sure the ECR repository %s exists and your
service serves /healthz, /readyz, and /metrics on port %d.
`, s.PlatformRepo, s.ECRRepository(), s.Port)
}
