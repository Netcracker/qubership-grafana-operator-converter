# Generated code — do not edit

Everything under this directory is `client-gen`, `lister-gen` and `informer-gen` output. Hand edits are lost on
the next regeneration and are invisible in review because nobody reads generated files.

To change it, change the types in `api/operator/` and regenerate with the `api-gen-v1alpha1` / `api-gen-v1beta1`
Makefile targets, which delete and rewrite this tree.

Two things that cost other agents time: the fake clientset entry point is in `fake/clientset_generated.go`, not
`clientset.go`, and its constructor is `NewSimpleClientset`. See the root `AGENTS.md` before writing any probe
over these fakes — they diverge from a real API server in ways that produce both false positives and false
refutations.
