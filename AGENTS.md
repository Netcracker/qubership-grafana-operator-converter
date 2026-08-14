# Working in this repository

Notes for AI agents, distilled from reviews in which several dozen agents worked this repository independently.
Everything below is something more than one of them got wrong or lost time to. It is not a style guide and not a
list of known defects — it is the set of traps this repository sets.

These are observations, not policy. If one of them stops being true, correct it here.

Scoped notes live next to the code they describe: `controllers/`, `charts/*/`, `api/operator/`, `api/client/`.

## What this repository is, and what it is not

A single-purpose controller that watches the legacy Grafana resources and creates the equivalent current ones. The
hand-written product is about a thousand lines: the entry point, the manager package, and the converters.

**It does not own the API it serves.** The API type packages are copies of two upstream operator releases, the
client packages are code-generator output, and the CRDs shipped in the chart are regenerated from those copies.
They belong to API groups this project does not control. Before reporting anything about a CRD field's name, type,
or optionality, check whether the decision is even made here — usually it was made upstream.

Consequences for everyday work:

- Generated and copied code outnumbers the product by an order of magnitude, so an unqualified `grep -r` drowns and
  every whole-tree metric — coverage, lint counts, "files nobody reviewed" — describes the copies rather than the
  product. Scope each of them to the hand-written packages first.
- The two `grafanas` CRDs are orders of magnitude larger than the rest. Never read either whole.
- The copies contain code with no caller in the product path. Presence in the tree is not evidence of use.

## Build and test

Build and test with `go` directly.

**Avoid the `make` targets for routine work.** The generation target post-processes with `sed` **against the
chart's real CRD directory**, the format target rewrites sources, and the test target depends on generation — so
"just running the tests" modifies your working tree. The Makefile also resolves its tools through `which` and
installs them on demand, so the versions pinned at the top are advisory: whatever is on `PATH` wins, and a local
run can rewrite the chart with a different tool's output.

**A green test run means very little here.** The product packages have no test files, so a test or race run over
them prints `[no test files]` and exits 0. The coverage figure the repository reports comes from tests inside the
copied upstream packages, and the CI coverage scope does not even include every hand-written package. Verify this
is still true before relying on it — but never cite a green race run as evidence that concurrent code is sound. Any
claim about behavior needs a probe you write yourself.

**The first compile in a fresh checkout or worktree takes minutes**, because of the generated fake clientsets and
the Kubernetes client dependency tree. Background it, and do not read a two-minute command timeout as a hang.

Tools installed with `go install` land in `$(go env GOPATH)/bin`, which is often not on `PATH`.

## What CI actually checks

**The checks are not in this repository.** The workflow files here are thin callers into reusable workflows owned
by sibling organization repositories and pinned by SHA, and the linter configuration is fetched at run time from an
organization-wide repository on a moving branch. Reading the local YAML tells you almost nothing. Fetch the
referenced workflow at its pinned revision before claiming that CI does or does not enforce something. When the
platform API is slow or blocked, the vendor CLI usually still works where a plain HTTP request does not.

**A green check is compatible with the check never having run.** Several workflows are scoped so narrowly that a
pull request passes without exercising them: scanners on a schedule or manual dispatch only, a license check on
`push` that is invisible on fork pull requests, chart linting behind a path filter, and a chart-lint invocation
whose version-increment gate is switched off by flag over an empty examples directory. Before citing a workflow as
a gate, read its triggers, its path filters, and the flags on the command it runs.

## Version identity

**A running image cannot be traced to a commit.** The container build compiles a named source file rather than the
package, which makes the Go toolchain record no VCS stamp at all; no build injects version ldflags; the binary has
no version flag; and the repository has neither tags nor releases. The Makefile's version variable, the chart's
`appVersion`, and the published image tag are three disconnected things, and the chart's default image is a
bot-updated digest of a branch — installing the chart exercises whatever that branch last built, not the tree you
are reading. Build and side-load an image if you need to observe the code in front of you.

**The builder image disables automatic toolchain switching**, so the toolchain directive in `go.mod` — including a
security bump to it — has no effect on the shipped binary, and the image tag alone decides the compiler. Any
standard-library scan run with the host default answers a different question than the one about the shipped
artifact. Pin the scan to the builder's toolchain.

## Running the binary locally

**The kubeconfig flag does not work.** The manager registers its own flag only when one is not already registered,
loses that race with the controller-runtime flag of the same name, and captures an empty value — so the binary
always takes the in-cluster path no matter what you pass, and fails with an in-cluster configuration error. Build
the manager yourself in a probe rather than exercising the real entry point, and pick non-default metrics and probe
ports.

**The manager is a shell.** It carries namespace-scoping and cache options that read like the authoritative watch
configuration, and nothing consumes them: the converters build their own informer factories directly from the
clientset. Reasoning about watch scope, RBAC breadth, or the framework's standard reconcile metrics from the
manager setup produces confident conclusions about a cache no code reads. Grep for consumers of the manager's
client and cache before you start.

**Core behavior is gated on an optional config file whose absence is not an error.** A missing or malformed file
yields an empty configuration with a nil error, every per-kind switch off, no informers registered — and a process
that starts, reports healthy, and does nothing. A clean startup log is not evidence the program is working.

## CRD regeneration: the drift is not what it looks like

Two traps stack here.

First, the generator's output never matches the committed files byte for byte, because the Makefile injects Helm
hook annotations afterwards. Filter those lines out before diffing, or every file appears to differ.

Second, once filtered, two of the shipped CRDs still differ — and every hunk sits inside `description` text
inherited from a different Kubernetes API library vintage, with no schema property changed. Several agents found
this independently and nearly reported it as schema drift. The same applies to the generated deep-copy file, where
the difference is a build tag and a header. Confirm a structural change before calling anything drift.

Regenerate into a scratch directory, never over the chart. Nothing in CI regenerates or diffs, and regeneration
exits 0 whether or not output changed, so the only signal is the diff you choose to read.

## Linting and scanning

**The lint configuration is not where you will look for it, and there is more than one.** The Go linter config
lives under the CI linters directory by super-linter convention — a root-level search misses it, and so does the
linter itself, which only searches the working directory and its parents. Part of the rule set is fetched from
another repository at run time. A local run therefore reproduces neither the checked-in config nor the CI rules.

**Lint is red at baseline**, with deprecation warnings in the copied upstream files, and no workflow runs it. Gate
on "no new issues", never on a clean run — and read the exit status of the command itself rather than of a
pipeline. A stray `| tail` reports the pager's status, and has already put a false "lint is green" into a report
that survived several review passes.

Three more:

- The license checker currently fails on this module and prints no rows. Zero rows is a tool failure, not a clean
  tree. The project's real gate is its allowlist file, enforced by a separate workflow.
- A manifest validator has no schema for custom resource *definitions* in its bundled set and reports one error per
  CRD file. That is the tool, not the chart. The rendered workload does validate.
- The scanner-suppression file, and any skip entry in a linter config, is not evidence that the underlying rule
  would fire. Some skips predate the manifests or match patterns the chart never produces. Run the check before
  citing a suppression as a known violation.

## Reading the upstream projects

Both copied APIs are fetchable through the Go module proxy, but **not from inside this module** — a download there
fails with `not a known dependency`. Create a scratch module in a temporary directory and download into the module
cache from there.

Mind the module paths: the two upstream majors live under **different** module paths, and the vendor-named module
does not exist on the proxy at all. An agent concluded from that 404, plus a hung `git ls-remote`, that upstream was
unreachable — and two findings were overstated as regressions until someone read the older major and found the same
behavior there. If direct git or HTTP to the code host stalls, the vendor CLI and the module proxy both work.

## Working-tree hygiene

**A clean `git status` is not proof of a clean tree.** Two independent reasons here:

- A bare binary name in `.gitignore` also matches the same-named **source package directory**. A new file placed
  there — a probe test, for instance — never appears in `git status`, is skipped by `git add .`, and is absent from
  file inventories. Already-tracked files in that package are unaffected, which is what makes it hard to notice.
- A git worktree rooted inside the repository stays registered after the process that created it goes away. Check
  `git worktree list` as well, and look for leftovers from earlier runs before concluding anything from tree state.

Anything you drop under the repository root is invisible to `git status` if it is ignored, yet fully visible to
`grep -r`, security scanners, and Go's package loader — stray `.go` files there break analysis with `undefined:`
errors from packages that were never meant to compile, and downloaded third-party sources in a scratch directory
get swept into whole-tree scans. Scope analyzers to the product packages explicitly, and prefer a worktree outside
the repository for anything that has to build.
