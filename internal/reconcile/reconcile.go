// Package reconcile turns a desired Skill×Target selection into a Plan: the
// concrete set of install / uninstall / reinstall operations (plus detected
// Conflicts) that Apply will carry out. It is a pure function over data — no
// disk, no network — so it is fully testable. See CONTEXT.md (Apply, Plan).
package reconcile

import (
	"sort"

	"github.com/earada/skillmux/internal/domain"
)

// Cell is one coordinate of the desired selection: a Skill (identified by its
// Name and the Source it comes from) wanted in a Target.
type Cell struct {
	Skill  string
	Source string
	Target string
}

// AvailableSkill is a Skill discovered in a Source together with the current
// content fingerprint of its folder (computed from the cached Source).
type AvailableSkill struct {
	Name        string
	Source      string
	Fingerprint string
}

// OpKind is the kind of operation an Apply will perform.
type OpKind string

const (
	Install   OpKind = "install"
	Uninstall OpKind = "uninstall"
	Reinstall OpKind = "reinstall"
	// Conflict is not an action but a reported blocker: it is emitted when a
	// Plan cannot be formed for a (Target, Skill name) because two Sources
	// claim it. The user must resolve it before those Skills can install.
	Conflict OpKind = "conflict"
)

// Reasons explain why a Reinstall is needed.
const (
	ReasonUpdateAvailable = "update-available"
	ReasonSourceChanged   = "source-changed"
)

// Operation is a single line of a Plan.
type Operation struct {
	Kind       OpKind
	SkillName  string
	SourceName string
	TargetName string
	Reason     string
}

// Plan is the ordered preview shown before Apply.
type Plan struct {
	Operations []Operation
}

// Reconcile computes the Plan that brings the installed state in line with the
// desired selection, given the currently available Skills (and their current
// fingerprints) and the recorded Installations.
func Reconcile(desired []Cell, available []AvailableSkill, installed []domain.Installation) Plan {
	fingerprints := map[key]string{} // (name, source) -> current fingerprint
	for _, a := range available {
		fingerprints[key{a.Name, a.Source}] = a.Fingerprint
	}

	// Detect conflicts: within one Target, the same Skill name desired from
	// more than one Source maps to the same install folder and cannot coexist.
	sourcesPerNameTarget := map[nameTarget]map[string]bool{}
	for _, c := range desired {
		nt := nameTarget{c.Skill, c.Target}
		if sourcesPerNameTarget[nt] == nil {
			sourcesPerNameTarget[nt] = map[string]bool{}
		}
		sourcesPerNameTarget[nt][c.Source] = true
	}
	conflicted := map[nameTarget]bool{}
	var plan Plan
	for nt, sources := range sourcesPerNameTarget {
		if len(sources) > 1 {
			conflicted[nt] = true
			plan.Operations = append(plan.Operations, Operation{
				Kind: Conflict, SkillName: nt.name, TargetName: nt.target,
			})
		}
	}

	// Index the desired selection (excluding conflicted cells) and the
	// installed state for quick membership tests.
	desiredAt := map[targetSkill]Cell{} // (target, name) -> chosen cell
	for _, c := range desired {
		if conflicted[nameTarget{c.Skill, c.Target}] {
			continue
		}
		desiredAt[targetSkill{c.Target, c.Skill}] = c
	}
	installedAt := map[targetSkill]domain.Installation{}
	for _, in := range installed {
		installedAt[targetSkill{in.TargetName, in.SkillName}] = in
	}

	// Installs / reinstalls / no-ops for everything desired.
	for ts, c := range desiredAt {
		in, isInstalled := installedAt[ts]
		if !isInstalled {
			plan.Operations = append(plan.Operations, Operation{
				Kind: Install, SkillName: c.Skill, SourceName: c.Source, TargetName: c.Target,
			})
			continue
		}
		if in.SourceName != c.Source {
			plan.Operations = append(plan.Operations, Operation{
				Kind: Reinstall, SkillName: c.Skill, SourceName: c.Source,
				TargetName: c.Target, Reason: ReasonSourceChanged,
			})
			continue
		}
		if in.Fingerprint != fingerprints[key{c.Skill, c.Source}] {
			plan.Operations = append(plan.Operations, Operation{
				Kind: Reinstall, SkillName: c.Skill, SourceName: c.Source,
				TargetName: c.Target, Reason: ReasonUpdateAvailable,
			})
		}
		// else: up to date, no operation
	}

	// Uninstalls for everything installed but no longer desired.
	for ts, in := range installedAt {
		if _, want := desiredAt[ts]; !want {
			plan.Operations = append(plan.Operations, Operation{
				Kind: Uninstall, SkillName: in.SkillName, SourceName: in.SourceName, TargetName: in.TargetName,
			})
		}
	}

	sortOperations(plan.Operations)
	return plan
}

type key struct{ name, source string }
type nameTarget struct{ name, target string }
type targetSkill struct{ target, skill string }

// sortOperations gives the Plan a stable, human-scannable order: by target,
// then skill, then kind. Map iteration above is non-deterministic, so this is
// what makes the preview reproducible.
func sortOperations(ops []Operation) {
	sort.Slice(ops, func(i, j int) bool {
		a, b := ops[i], ops[j]
		if a.TargetName != b.TargetName {
			return a.TargetName < b.TargetName
		}
		if a.SkillName != b.SkillName {
			return a.SkillName < b.SkillName
		}
		return a.Kind < b.Kind
	})
}
