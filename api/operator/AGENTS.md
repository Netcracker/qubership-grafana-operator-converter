# Copied upstream types — this project does not own them

These packages are copies of two majors of an upstream operator's API. Editing them here forks a schema this
project does not maintain: the CRDs shipped in the chart are regenerated from these files, so a local change
silently ships someone else's API with your modification in it.

Design questions about these types — a field's name, type, optionality, or removal between versions — were decided
upstream. Route them there rather than reporting them as defects here. Only *divergence from upstream* is a finding
against this repository.

**Nothing records which upstream release the copies came from**, and upstream is not a module dependency, so
nothing resolves or diffs it. The copies are also hand-modified rather than a clean snapshot: some files differ
substantially from every upstream release, and most have had their license headers stripped. Any claim that these
types match a particular upstream version is only as good as a source-tree diff against that version fetched from
the module proxy — a CRD-schema comparison cannot see edits that do not affect schema generation. Fetch from a
scratch module outside this one, and expect the module path to differ between the two majors.

**The Go package names do not line up with the API groups they declare.** The package named for the older version
holds the legacy group, and the package named for the newer one holds the current group. Two agents read the
group-version file and the CRD file names as contradicting each other. Settle which package produces which CRD by
running the generator over each package in isolation into a scratch directory.

The deep-copy file is generator output rather than part of the copy — and it has no regeneration target and no
drift check, so it goes stale whenever a dependency adds a reference-typed field. Regenerating it by hand also
strips the license header, so the diff mixes cosmetic damage with the real change; read it hunk by hunk.
