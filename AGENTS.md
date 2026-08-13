# Working in this repository

Notes for AI agents, distilled from a review in which about thirty agents worked this repository
independently. Everything below is something several of them got wrong or lost time to. It is not a style
guide and not a list of known defects — it is the set of traps this repository sets.

These are observations, not policy. If one of them stops being true, correct it here.

## What this repository is, and what it is not

A single-purpose controller that watches `integreatly.org/v1alpha1` Grafana resources and creates the
equivalent `grafana.integreatly.org/v1beta1` ones. The hand-written product is about a thousand lines:
`main.go`, `manager/manager.go`, and the converters in `controllers/`.

**It does not own the API it serves.** `api/operator/v1alpha1/` is a copy of grafana-operator v4 and
`api/operator/v1beta1/` a copy of v5; `api/client/**` is `client-gen` output; the CRDs shipped in the chart
are regenerated from those copies and belong to two API groups this project does not control. Before
reporting anything about a CRD field's name, type, or optionality, check whether the decision is even made
here — usually it was made upstream.

Consequences for everyday work:

- Generated and copied code outnumbers the product by an order of magnitude, so an unqualified `grep -r`
  drowns. Scope to `controllers/ manager/ main.go`, or exclude `api/client/` from the file list.
- The two `grafanas` CRDs are two orders of magnitude larger than the rest. Never read either whole.
- **Only four kinds are converted**: dashboards, datasources, folders, notification channels — one
  `AddEventHandler` each in the controller. Despite the file name, there is **no converter for kind
  `Grafana`**, and the two `grafanas` CRDs the chart ships are for that unconverted kind. Several agents
  assumed a converter for it exists.

## Build and test

Build and test with `go` directly.

**Avoid the `make` targets for routine work.** `make test` depends on `generate`, whose hook-annotation step
runs `sed` **against the chart's real `crds/` directory**, and `make fmt` rewrites sources — so "just running
the tests" modifies your working tree. The Makefile also resolves its tools through `which`, so the versions
pinned at the top are advisory: whatever is on `PATH` wins.

**A green test run means very little here.** The product packages `controllers/` and `manager/` have no test
files, so `go test ./controllers/... ./manager/... -race` prints `[no test files]` and exits 0. Whatever
coverage the repository reports comes from tests inside the copied upstream packages. Verify this is still
true before relying on it — but never cite a green race run as evidence that concurrent code is sound. Any
claim about behaviour needs a probe you write yourself.

Tools installed with `go install` land in `$(go env GOPATH)/bin`, which is often not on `PATH`.

## Writing probes against the converter

The generated fake clientsets are the cheapest harness, and they have three sharp edges.

**Informer-driven probes hang forever.** Anything that drives a real `SharedInformerFactory` over a fake
clientset blocks in `WaitForCacheSync`, logging `awaiting required bookmark event for initial events stream`
on a timer. Current client-go enables the `WatchListClient` feature gate by default and the fake watcher never
sends the bookmark it waits for. Disable the gate for the run:

```bash
KUBE_FEATURE_WatchListClient=false go test ./controllers/ -run 'TestX' -v
```

Direct handler calls need no gate; a subprocess spawned from a test inherits it through the environment.
Equally, **do not call `ConverterController.Start`** in a probe — it waits for cache sync and never returns.
Start the informer factories yourself and read the fake clientset's recorded actions.

**The fake is not an API server, and it misleads in both directions.** It does not evaluate CEL
(`x-kubernetes-validations`), does not apply defaulting or pruning, does not enforce immutability, and its
error paths differ from the real typed client — on an `Update` error it returns a nil object where the real
client returns a non-nil empty one. In one review this produced a nil-dereference "defect" that does not
exist, and a refutation of a real immutability defect that does. So: a defect seen only against the fake is
unproven, and a defect *refuted* only against the fake is not refuted.

**Anything about validation, defaulting, immutability or admission needs a real API server.** envtest gives
one as local processes, without Docker or a cluster: install `setup-envtest` matching the project's
controller-runtime, point `KUBEBUILDER_ASSETS` at the downloaded assets, and construct an
`envtest.Environment` whose `CRDDirectoryPaths` is the chart's `crds/` directory. The converter is an ordinary
client-go program, so pointing it at the resulting `rest.Config` exercises the real thing end to end. The
probe must live in `package controllers` — the event handlers are unexported.

The CEL validator can also be used on its own, but the apiextensions CEL package is absent from `go.sum`
because nothing in the module imports that subtree; adding it edits `go.mod` and `go.sum`. Do that in a
throwaway worktree, and check `git diff go.mod` before drawing any conclusion about dependencies.

## The Helm chart

**`helm template` hides every CRD.** They live in `crds/`, which the renderer skips unless you pass
`--include-crds`. Any question about CRD ownership, upgrade behaviour, or `helm.sh/resource-policy` answered
from the plain render is answering about a different chart.

The `helm.sh/hook: crd-install` annotations the Makefile injects are **inert** where the files sit; they would
take effect — and then render no CRDs at all — only if a file were moved into `templates/`.

Two mechanical traps:

- A comma-separated `--set` value such as the watched-namespace list is split by Helm into separate
  assignments and fails with a message naming a key you never wrote. Escape the comma: `a\,b`.
- Never pipe `helm template` through `2>/dev/null`. Recent Helm enforces a release-name regex and prints
  template errors on stderr; suppressing it turns a failed render into what looks like an empty one.

## CRD regeneration: the drift is not what it looks like

Regenerating the CRDs with the project's pinned `controller-gen` reports that two of them differ — both
`grafanas` files. Four agents found this independently and nearly reported it as schema drift. Every hunk is
inside a `description` string inherited from a different `k8s.io/api` vintage; no schema property changes.
The same applies to the generated deep-copy file, where the difference is a build tag and a header.

Regenerate into a scratch directory, never over the chart, and compare ignoring the injected hook
annotations.

## Linting and scanning

**The lint configuration is not where you will look for it, and there is more than one.** The `golangci`
config lives under `.github/linters/` by super-linter convention — a root-level search misses it, and so does
`golangci-lint` itself, which only searches the working directory and its parents. The rules CI actually
enforces are checked out **from another repository**: the super-linter workflow sparse-checks the
organisation's `.github` repository for a shared linter config, on a moving branch. The same local directory
holds the configs for the other linters.

**Lint is red at baseline**, with deprecation warnings in the copied upstream files. Gate on "no new issues",
never on a clean run — and read the exit status of the command itself rather than of a pipeline. A stray
`| tail` reports the pager's status, and has already put a false "lint is green" into a report that survived
several review passes.

**Vulnerability scans answer differently depending on the toolchain.** The Dockerfile pins an older Go than a
developer machine is likely to have, and the official images set `GOTOOLCHAIN=local`, so the build never
upgrades to the version named in `go.mod`. Scan with the toolchain the image actually uses, not the host's,
or you will report the wrong answer in either direction.

Three more:

- `go-licenses` currently fails on this module and prints no rows. Zero rows is a tool failure, not a clean
  tree. The project's real gate is the `.wwhrd.yml` allowlist, enforced by its own workflow.
- `kubeconform` cannot validate the CRD files themselves — it has no schema for `CustomResourceDefinition` in
  its bundled set and reports one error per file. That is the tool, not the chart. The rendered workload does
  validate.
- `.qubership/grand-report.json` is a **scanner-suppression file**. Read it before concluding that a scan is
  clean.

## Reading the upstream projects

Both copied APIs are fetchable through the Go module proxy, but **not from inside this module** — a download
there fails with `not a known dependency`. Create a scratch module in a temporary directory and download into
the module cache from there.

Mind the module paths: the v4 project (the source of the v1alpha1 types) and the v5 project (the source of the
v1beta1 types) live under **different** module paths, and the Netcracker-named module does not exist on the
proxy at all. An agent concluded from that 404, plus a hung `git ls-remote`, that upstream was unreachable —
and two findings were overstated as regressions until someone read v4 and found the same behaviour there. If
direct git or `curl` to GitHub stalls, `gh api` and the module proxy both work.

## Scratch files

Anything you drop under the repository root is invisible to `git status` if it is ignored, yet fully visible
to `grep -r`, `gosec`, and Go's package loader — stray `.go` files there break analysis with `undefined:`
errors from packages that were never meant to compile. Scope analysers to the product packages explicitly,
and prefer a `git worktree` outside the repository for anything that has to build. A clean `git status` does
not mean nothing is outstanding: check `git worktree list` too.
