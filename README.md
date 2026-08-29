# platform-cli

Scaffolds a new service onto the internal developer platform.

```bash
brew install alexpermiakov/tap/platform
platform init
```

## What it generates

```
out/<service>/
├── platform-repo/
│   └── argocd/applications/<env>/values/<service>.yaml   # onboards the service
└── service-repo/
    ├── .github/workflows/ci.yaml                          # build, scan, request deploy
    ├── Dockerfile                                         # non-root, read-only-rootfs ready
    └── README.md
```

Each scaffold gets its own subdirectory, since every service targets a
different repository.

Two destinations. The `platform-repo/` file goes to [paved-road](https://github.com/alexpermiakov/paved-road)
as a PR — merging it is what onboards the service. Everything under
`service-repo/` is committed next to your code.

```
$ platform init payment-processor --team payments --language node --ingress --env dev,prod
```

Run with no flags for the interactive wizard, or `--non-interactive` to use
flags only. `--dry-run` prints the files without writing them.

## Languages

`--language` picks the runtime: `go` (the default), `node`, or `python`. Common
spellings are accepted, so `nodejs`, `node.js`, `typescript`, and `py` all
resolve rather than failing validation.

It changes exactly two files:

| File | What the language decides |
|---|---|
| `.github/workflows/ci.yaml` | the `quality-gate` job — the toolchain, linter, and test runner |
| `Dockerfile` | the base images and the build, as a multi-stage build ending non-root |

Nothing else moves. The values file is byte-identical across all three, because
the runtime stops mattering at the image boundary — the chart only ever sees an
image reference. There is a test asserting exactly that.

Both generated files are starting points. The Dockerfile's `CMD` points at a
conventional entrypoint (`server.js`, `main:app`) that you are expected to
correct, and the Node and Python gates lean on `--if-present` and the usual
`requirements.txt` layout. Pass `--no-dockerfile` if the repo already has one:
`init` refuses to clobber existing files anyway, but the flag keeps a re-run
with `--force` from overwriting a Dockerfile you have since edited.

## What it deliberately does not generate

**An ArgoCD `Application`.** The platform's ApplicationSet already creates one
per values file, using a git file generator over
`argocd/applications/<env>/values/*.yaml`. A hand-written Application would be
a second owner of the same namespace, and with `prune: true` on both sides they
delete each other's resources.

**A Helm chart.** Every service uses the platform's `standard-service` chart.
The values file is the entire interface.

**Raw cpu/memory limits.** Services pick `small`, `medium`, or `large`. The
platform team owns what those mean, so capacity can be retuned centrally
instead of across forty bespoke values files.

**A `replicaCount`.** The HPA owns replicas, and `standard-service` 2.0.0
rejects the key outright.

## Chart versions

By default a service tracks whatever `standard-service` version the platform's
ApplicationSet carries, so chart releases reach it automatically. Pin it before
a risky upgrade:

```bash
platform init payment-processor --team payments --chart-version 2.0.0
```

That writes `chartVersion: 2.0.0` into the values file, which the ApplicationSet
reads to resolve the chart. Remove the line to go back to tracking the default.

## Rules it encodes

A scaffolder's job is to make the invalid states unreachable:

| Rule | Why |
|---|---|
| Name is a valid RFC 1123 label, ≤63 chars | It becomes the namespace |
| Values filename == `app.name` | The ApplicationSet derives the Application name and namespace from the filename, the workload name from `app.name`; if they diverge you get a confusing split |
| No ingress ⟹ `rollout.strategy: blueGreen` | The chart hard-fails a canary without an ingress, since the ALB does the traffic splitting |
| `app.team` is required | Platform policy labels every workload with its owner |
| CI mints a GitHub App token | Scoped and short-lived, rather than a long-lived PAT |
| `app.team` is always set | Required by the chart's `values.schema.json`; it labels every workload for cost attribution and admission policy |
| `chartVersion` is semver when pinned | It resolves an OCI chart tag, so a malformed value fails at sync rather than at render |
| Every Dockerfile ends non-root | The chart sets `runAsNonRoot`, so a root image fails admission instead of starting |
| The runtime never reaches the values file | The chart takes an image reference; a language-dependent values file would be a leak of build concerns into deploy |

## Development

```bash
go test ./...
go run . init demo --team platform --non-interactive --out /tmp/demo
```

Templates live in `internal/scaffold/templates/` and are compiled into the
binary with `go:embed`, so a release is always self-consistent — there is no
way to run new code against old templates.

They use `{% %}` delimiters. `{{ }}` is unavailable: the output is Helm values
and GitHub Actions workflows, which both use it natively, and the CI template
also contains bash `[[ ]]` conditionals.

Adding a language is three things: a `quality_<lang>.tmpl` partial, a
`dockerfile_<lang>.tmpl`, and an entry in `scaffold.Languages`. The whole
template directory is parsed as one set, so `ci.yaml.tmpl` pulls in the right
partial by name; the Dockerfile is looked up as `dockerfile_<language>.tmpl`.
The table-driven tests iterate `Languages`, so a missing template fails the
suite rather than reaching a user.

CI scaffolds one service per language and then checks all three the way their
consumers would: the values go through the real `standard-service` chart, the
workflows through `actionlint`, and the Dockerfiles through `hadolint`. A chart
change that breaks the scaffold, or a partial spliced in at the wrong
indentation, fails here rather than at ArgoCD sync.
