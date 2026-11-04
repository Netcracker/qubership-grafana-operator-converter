# Generated code — do not edit

Everything under this directory is client, lister, and informer generator output. Hand edits are lost on the next
regeneration and are invisible in review because nobody reads generated files.

To change it, change the types in the sibling API directory and regenerate with the per-version generation targets,
which delete and rewrite this tree. Those targets are **not** reachable from the umbrella generation target, so
these clients are never regenerated or checked by anything routine.

This tree dominates every whole-tree measurement — file counts, coverage, lint totals. Exclude it before quoting a
number about the product.

Two things that cost other agents time: the generated files use the older generator's naming, so list the directory
rather than guessing a conventional file name, and compiling this tree is what makes the first build in a fresh
checkout take minutes.

**Before writing any probe over the fake clientsets, read `controllers/AGENTS.md`.** They are the cheapest harness
in the repository and they diverge from a real API server — and from the real generated client — in ways that have
already produced both a false defect and a false refutation in this project.
