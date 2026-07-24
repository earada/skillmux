package tui

import "github.com/earada/skillmux/internal/safetext"

// Terminal text sanitization at the view boundary. Every string the views
// interpolate that skillmux does not itself author — Skill names, descriptions,
// folder groups, file names, raw file contents, diff lines, git revision labels,
// Source/subprocess errors — passes through one of these before it reaches Lip
// Gloss or Glamour. See internal/safetext for what is stripped and why.

// sanitize renders untrusted text as an inert single line: use it for names,
// descriptions, paths, labels, and one-line error strings.
func sanitize(s string) string { return safetext.Line(s) }

// sanitizeMultiline is sanitize for text a viewport scrolls over many lines —
// raw file bodies and the markdown fed to Glamour. It keeps '\n' and '\t'.
func sanitizeMultiline(s string) string { return safetext.Multiline(s) }
