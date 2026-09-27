// Package unifiedcheck checks every constituent project of a unified
// documentation site in one pass.
//
// A unified site is built from several projects plus the docs-site's own
// cross-cutting content, each with its own selfdoc.json and its own docs tree.
// [CheckUnified] runs the ordinary documentation check over each of them and
// merges the answers into one verdict, attributing every diagnostic, every
// directive result and every covered symbol to the project it came from by
// prefixing the project's mount slug -- "[core] docs/index.md" -- so a report
// over a monorepo names a file a reader can open.
//
// # A project the check cannot reach is a diagnostic, not a crash
//
// Two conditions belong to the unified run rather than to any one project:
// a constituent directory carrying no selfdoc.json (unified-project-without-config) and a
// constituent whose check refuses to run at all (unified-project-check-failed, carrying the
// refusal's own message). Either one is recorded against the project and the
// remaining projects are still checked, because a monorepo's report is worth
// more than the first thing wrong with it.
package unifiedcheck
