# TFLint config for the platform's Terraform (applied recursively to all modules
# under infra/). Uses the bundled terraform ruleset at its "recommended" preset.
#
# CI gates on `error` severity only (--minimum-failure-severity=error). The
# current style warnings — missing required_version, unpinned provider versions,
# a few unused vars wired via TF_VAR_ in CI — are surfaced for visibility and
# tracked as a cleanup backlog, not yet blocking. Tighten by removing the
# severity floor once those are addressed.
plugin "terraform" {
  enabled = true
  preset  = "recommended"
}
