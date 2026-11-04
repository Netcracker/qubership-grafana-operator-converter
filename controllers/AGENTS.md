# The converters

This package and the manager package are the hand-written product. Everything under `api/` is copied or generated.
Read the root `AGENTS.md` first — the build, CI, and working-tree traps there apply here.

## What is actually converted

**Only four kinds are converted**, one event handler each. Despite the file naming, there is **no converter for
the `Grafana` kind**, and the largest CRDs the chart ships are for that unconverted kind. Several agents assumed a
converter for it exists. The kubebuilder project metadata and several file names still describe the upstream
project rather than this one, so confirm from the code which kinds a controller handles rather than from a file
name or a summary.

## Writing probes

These packages have no tests, so any behavioral claim needs a probe you write. The generated fake clientsets are
the cheapest harness, and they have sharp edges.

**Informer-driven probes hang forever.** Anything that drives a real shared informer factory over a fake clientset
blocks in cache sync, because the current client-go watch-list client waits for a bookmark event the fake watcher
never sends. The symptom is no error, no output, and no completion — a probe that neither fails nor finishes is
this, not your test logic. Disable the feature gate through the environment on the outer test process; a spawned
subprocess inherits it. Direct handler calls need no gate.

**Do not call the controller's `Start`** in a probe — it waits for cache sync and never returns. Start the informer
factories yourself and read the fake clientset's recorded actions.

**Configuration derived from the environment is memoized in package-level once-guards**, so the first scenario in a
test binary decides the answer for every later one. Two scenarios agreeing suspiciously is a signal, not a
confirmation. Run each scenario as its own process.

The probe must live in this package — the event handlers are unexported.

## What the fake clientset will not tell you

**It is not an API server, and it misleads in both directions.** It does not evaluate CEL validation rules, does
not apply defaulting or pruning, does not enforce immutability, does not validate names, and deep-copies objects on
write. Its error paths differ from the real typed client: on an update error it returns a nil object where the real
generated client returns a pre-allocated zero value, and it discards the context.

That combination has already cost this project real accuracy. Three review axes independently reported a
nil-dereference panic that only the fake can produce, each marked as executed. In the other direction, a probe on
the fake "refuted" an immutability defect that the shipped CRD's transition rule does in fact reject. And because
the fake deep-copies, an object-aliasing bug cannot be reproduced through it at all.

So: a defect seen only against the fake is unproven, and a defect *refuted* only against the fake is not refuted.
Before reporting a panic or an error-path behavior, read the real generated client's method next to the fake's.
When the claim is about serialization or error returns, drive the generated clientset against a local HTTP test
server. Anything about validation, defaulting, immutability, or admission needs a real API server — envtest gives
one as local processes, with the chart's CRD directory as the schema source.

The CEL validator can be used on its own, but the validator package is absent from `go.sum` because nothing in the
module imports that subtree; adding it edits the module files. Do that in a throwaway worktree, and check the diff
before drawing any conclusion about dependencies.

## Auditing conversion fidelity

**A field missing from the output is not evidence the converter dropped it.** The target API is a different major
version of a foreign schema: fields are removed upstream, relocated into nested secret or reference structures, and
pruned by the API server when unknown. A "dropped fields" list was rewritten three times across review passes
before someone counted the keys in the shipped CRDs and found that most of the supposedly dropped fields do not
exist in the target schema at all.

Check the target schema and the upstream migration path before calling something data loss — and check the
relocation target before calling it clean. The genuine defect in that family is the inverse: a value that has a
home in the new schema and is never mapped into it, while the conversion logs success.

Serialization adds a second false signal: the copied upstream types omit `omitempty` on nested non-pointer structs,
so marshalling emits zero-valued fields that never came from the source object.

## Configuration

The operator's YAML configuration is decoded through the struct's **JSON** tags, and unknown keys are tolerated. A
key spelled after the Go field name, after YAML convention, or with a typo is accepted and silently ignored. Every
per-kind switch defaults to on, so a mistyped key in the *disable* direction is a complete no-op. Verify a key
against the struct tag, and assert on the program's parsed configuration rather than on the process staying up.
