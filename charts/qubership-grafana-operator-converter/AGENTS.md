# The Helm chart

Read the root `AGENTS.md` first. This file covers the traps that are specific to the chart.

## The chart descends from the upstream operator's chart

Helper names, labels, value descriptions, and commented examples were inherited and still describe the upstream
component. A value or environment variable documented here may be inert: the binary reads only a handful of
environment variables, and none of the upstream-style image-override variables among them. A leader-election knob
discusses running several replicas while the deployment hard-codes one. Confirm a knob against the binary's own
reads before treating it as functional.

## The README is generated and stale

It is docs-generator output with a template beside it, and nothing regenerates or verifies it — no target, no CI
job, no pre-commit hook. It lags the values file and the chart metadata it claims to document, down to the version
badge and the image repository.

Worse, the generator only emits values carrying its comment marker, so the whole converter configuration block —
the chart's actual published contract — is missing from the values table rather than shown as undocumented. Read
the values file as the source of truth, and run the generator before making any claim about what it does or does
not emit.

## Values are unvalidated at every layer

There is no values schema, and the config template pipes the user's block through `toYaml` wholesale, so any key
renders into the config object unchallenged. The program then ignores unknown keys. A `--set` that renders and a
pod that starts prove nothing about whether your setting was understood; read the rendered config object and the
program's own echo of what it parsed.

Three mechanical traps in the same area:

- Where a default is a **non-empty map**, values files and `--set` merge into it rather than replacing it. Setting
  a label selector the obvious way yields the union of both and an AND-selector that matches nothing. The escape is
  the `null` trick.
- A comma-separated value is split by Helm into separate assignments and fails naming a key you never wrote.
  Escape the comma.
- Never pipe a render through `2>/dev/null`. Recent Helm enforces a release-name regex and prints template errors
  on stderr; suppressing it turns a failed render into what looks like an empty one. Verify the installed Helm
  major before relying on any habit from an earlier one.

## The CRD directory is not this project's API, and Helm barely manages it

**A default render hides every CRD.** They live in the CRD directory, which the renderer skips unless you ask for
them explicitly. Any question about CRD ownership, upgrade behavior, or resource policy answered from a plain
render is answering about a different chart. A schema validator pointed at rendered output therefore checks only
the workload.

The files there are a pinned copy of an upstream operator's CRDs for two API groups this project does not own.
Helm creates them when absent and **never upgrades them**, so whichever chart installs first freezes the schema for
the whole cluster, and fields added upstream later are pruned from user objects.

The hook annotations the generator injects into those files are **inert** where they sit — that directory bypasses
templating and hook processing entirely. They would take effect, and then render nothing at all, only if a file
were moved into the templates directory. Do not read them as working CRD lifecycle management, and do not "fix"
the upgrade problem by moving the files. Regenerating re-adds the annotations.

The CRDs are also large enough that a client-side apply can fail on the last-applied-configuration size cap; use
server-side apply, and distinguish the two when reporting install behavior.

## The documented install command does not work

The release name in the documentation does not contain the chart name, so the fullname helper concatenates both and
a generated service name lands over the 63-character limit. `helm lint` reports it as a warning **and exits 0**, and
the chart CI job runs only linting, so a green chart check does not mean the rendered manifests are installable.
Read lint output rather than its exit code.

Use a short release name for any baseline you need to succeed, so an install failure is never mistaken for the
behavior you were trying to observe.

## A configuration-only upgrade does not reach the running pod

The deployment carries no checksum annotation over the config object, so changing converter values and upgrading
updates the config and leaves the pod running the old one — a change that looks ignored. Restart before concluding
a setting has no effect, and remember that on restart the controller replays every existing object, which both
repairs earlier output and rewrites it.
