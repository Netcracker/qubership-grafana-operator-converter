# Copied upstream types — this project does not own them

`v1alpha1/` is a copy of the grafana-operator v4 API and `v1beta1/` a copy of v5. Editing them here forks a
schema this project does not maintain: the CRDs shipped in the chart are regenerated from these files, so a
local change silently ships someone else's API with your modification in it.

Design questions about these types — a field's name, type, optionality, or removal between versions — were
decided upstream. Route them there rather than reporting them as defects here, and read upstream from the Go
module proxy in a scratch module rather than guessing; the two projects live under different module paths.

`zz_generated.deepcopy.go` is `controller-gen` output, not part of the copy.
