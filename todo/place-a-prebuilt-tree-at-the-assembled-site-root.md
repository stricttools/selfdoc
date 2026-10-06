# Place a pre-built tree at the assembled site's root

## Context

The owner decided that the family's domain serves a hand-built landing page
at its root, with each tool's documentation at its own path on the same
domain (for example `/rlsbl/`), and that the documentation assembly owns
both, so one build and one deployment produce the whole domain with no
Worker in front.

Today the assembly's front page is the home project's own selfdoc build.
The landing page is plain static HTML, CSS, and JavaScript produced by its
own generator in a separate repository, with no selfdoc build of its own.
The assembly has no way to carry such a tree.

## What to build

- A way for the assembly to take a pre-built static tree and place it at the
  site root, replacing the home project's generated front page, while every
  project's documentation keeps its path.
- Declared in the assembly's existing roster or configuration rather than a
  new input surface, unless the existing artifacts cannot carry it, in which
  case the proposal names what was rejected and why.
- The tree's origin must be pinned the way projects are: a released version
  or a recorded commit of the landing repository, never a moving branch.
- Collisions between the tree's paths and project paths (a tool page at
  `/tools/<slug>/` in the tree and documentation at `/<slug>/`) are refused,
  naming both owners.
- The assembled-site verification (internal references, roster agreement,
  project reachability) covers the tree's pages too, and the tree's links into
  documentation must resolve.
- Sitemaps, robots, feeds, and search include the tree's pages under the
  same rules as generated pages.

## Open decisions

- Whether the tree replaces the home project entirely, or the home project
  keeps its other pages (for example a personal CV) under its own path.
- How the landing site's noindex preview header is kept off the production
  deployment.

## Effort

Medium: one new roster or configuration field, an integration step in the
assembly, collision refusal, and verification coverage.
