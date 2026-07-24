package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/earada/skillmux/internal/domain"
)

// Palette. Hex colours are used so truecolor terminals get the intended look;
// lipgloss degrades gracefully on 256/16-colour terminals.
var (
	cBrand   = lipgloss.Color("#A78BFA") // violet accent
	cBrandFg = lipgloss.Color("#1A1626") // text on a brand background
	cDim     = lipgloss.Color("#6B7280") // secondary text
	cBorder  = lipgloss.Color("#3B3B4F") // panel borders
	cGreen   = lipgloss.Color("#4ADE80")
	cAmber   = lipgloss.Color("#FBBF24")
	cRed     = lipgloss.Color("#F87171")
)

var (
	// titleStyle is the brand "pill" in the header bar.
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(cBrandFg).Background(cBrand).Padding(0, 1)
	// headingStyle titles secondary screens (Plan, Result, …).
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(cBrand)
	dimStyle     = lipgloss.NewStyle().Foreground(cDim)
	errStyle     = lipgloss.NewStyle().Foreground(cRed)

	// brokenStyle paints a cell (or a dependency in the detail line) amber when
	// its Dependency closure is unsatisfied in that Target.
	brokenStyle = lipgloss.NewStyle().Foreground(cAmber)

	// keyStyle renders a keycap in the footer; keyDescStyle its description.
	keyStyle     = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	keyDescStyle = dimStyle

	// panelStyle is the rounded box that frames the matrix and other content.
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1)

	// cursorStyle highlights the focused matrix cell / list row.
	cursorStyle = lipgloss.NewStyle().Background(cBrand).Foreground(cBrandFg).Bold(true)

	// skillNameStyle makes the skill name — the row's eye-anchor — stand out
	// from the dimmed group hint and the Source.
	skillNameStyle = lipgloss.NewStyle().Foreground(cBrand).Bold(true)
	// conflictNameStyle flags a name offered by more than one Source.
	conflictNameStyle = lipgloss.NewStyle().Foreground(cRed).Bold(true)
	// groupStyle dims the folder-hierarchy hint shown after a skill name.
	groupStyle = dimStyle
	// deprecatedWordStyle reddens the word "deprecated" when it appears inside a
	// Skill's folder path, so a repo that buckets retired skills under a
	// `deprecated/` folder stands out even without a frontmatter flag.
	deprecatedWordStyle = lipgloss.NewStyle().Foreground(cRed)
	// deprecatedStyle marks a skill its author has retired: dimmed and struck
	// through so it reads as "still listed, but don't reach for it".
	deprecatedStyle = lipgloss.NewStyle().Foreground(cDim).Strikethrough(true)
	// deprecatedGlyph precedes a deprecated skill's name in the matrix.
	deprecatedGlyph = "⊘"

	// tableBorderStyle colours the matrix grid lines.
	tableBorderStyle = lipgloss.NewStyle().Foreground(cBorder)
	tableHeadStyle   = lipgloss.NewStyle().Bold(true).Foreground(cBrand).Align(lipgloss.Center)

	statusStyles = map[domain.Status]lipgloss.Style{
		domain.StatusNotInstalled:    dimStyle,
		domain.StatusUpToDate:        lipgloss.NewStyle().Foreground(cGreen),
		domain.StatusUpdateAvailable: lipgloss.NewStyle().Foreground(cAmber),
		domain.StatusConflict:        lipgloss.NewStyle().Foreground(cRed),
		domain.StatusUnavailable:     lipgloss.NewStyle().Foreground(cRed),
		domain.StatusModified:        lipgloss.NewStyle().Foreground(cRed),
	}
	statusGlyph = map[domain.Status]string{
		domain.StatusNotInstalled:    "·",
		domain.StatusUpToDate:        "=",
		domain.StatusUpdateAvailable: "↑",
		domain.StatusConflict:        "!",
		domain.StatusUnavailable:     "×",
		domain.StatusModified:        "≠",
	}
)
