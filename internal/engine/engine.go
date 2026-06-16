// Package engine wires the core together for the TUI: it loads Config and
// Manifest, refreshes the catalog of available Skills from the configured
// Sources (fetch + scan + fingerprint), computes the per-cell Status, and turns
// a desired Skill×Target selection into a Plan it can Apply and persist.
package engine

import (
	"os"
	"path/filepath"

	"github.com/earada/skillmux/internal/apply"
	"github.com/earada/skillmux/internal/config"
	"github.com/earada/skillmux/internal/domain"
	"github.com/earada/skillmux/internal/fetch"
	"github.com/earada/skillmux/internal/fingerprint"
	"github.com/earada/skillmux/internal/manifest"
	"github.com/earada/skillmux/internal/paths"
	"github.com/earada/skillmux/internal/reconcile"
	"github.com/earada/skillmux/internal/source"
)

// Engine holds the loaded state and dependencies.
type Engine struct {
	Config       *config.Config
	Manifest     *manifest.Manifest
	Fetcher      *fetch.Fetcher
	configPath   string
	manifestPath string
}

// New builds an Engine from already-loaded state. Used directly in tests.
// configPath may be empty when the caller will not mutate the Config.
func New(cfg *config.Config, man *manifest.Manifest, fetcher *fetch.Fetcher, configPath, manifestPath string) *Engine {
	return &Engine{Config: cfg, Manifest: man, Fetcher: fetcher, configPath: configPath, manifestPath: manifestPath}
}

// Load wires an Engine from the on-disk Config and Manifest at their XDG paths.
func Load() (*Engine, error) {
	configPath, err := paths.ConfigFile()
	if err != nil {
		return nil, err
	}
	manifestPath, err := paths.ManifestFile()
	if err != nil {
		return nil, err
	}
	cacheDir, err := paths.CacheDir()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	man, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	return New(cfg, man, &fetch.Fetcher{CacheDir: cacheDir}, configPath, manifestPath), nil
}

// AvailableSkill is a Skill discovered in a Source, with its current
// fingerprint and the local folder to install from.
type AvailableSkill struct {
	Name        string
	Source      string
	Description string
	Dir         string
	Fingerprint string
}

// Catalog is the result of a Refresh: the available Skills and any per-Source
// errors encountered (Refresh is best-effort across Sources).
type Catalog struct {
	Skills       []AvailableSkill
	SourceErrors map[string]error
}

// CellStatus is the Status of one (Skill, Target) cell in the matrix.
type CellStatus struct {
	SkillName  string
	SourceName string
	TargetName string
	Status     domain.Status
}

// Refresh fetches and scans every configured Source, computing the current
// fingerprint of each discovered Skill. Errors from one Source do not stop the
// others; they are collected in Catalog.SourceErrors.
func (e *Engine) Refresh() Catalog {
	cat := Catalog{SourceErrors: map[string]error{}}
	for _, src := range e.Config.DomainSources() {
		root, err := e.Fetcher.Fetch(src)
		if err != nil {
			cat.SourceErrors[src.Name] = err
			continue
		}
		skills, err := source.Scan(root, src.Name)
		if err != nil {
			cat.SourceErrors[src.Name] = err
			continue
		}
		for _, sk := range skills {
			dir := filepath.Join(root, sk.RelPath)
			fp, err := fingerprint.Dir(dir)
			if err != nil {
				cat.SourceErrors[src.Name] = err
				continue
			}
			cat.Skills = append(cat.Skills, AvailableSkill{
				Name:        sk.Name,
				Source:      sk.SourceName,
				Description: sk.Description,
				Dir:         dir,
				Fingerprint: fp,
			})
		}
	}
	e.saveCatalog(cat)
	return cat
}

// Status computes the Status of every (available Skill, Target) cell by
// comparing the recorded Installation against the Skill's current fingerprint.
func (e *Engine) Status(cat Catalog) []CellStatus {
	var out []CellStatus
	for _, t := range e.Config.DomainTargets() {
		for _, sk := range cat.Skills {
			out = append(out, CellStatus{
				SkillName:  sk.Name,
				SourceName: sk.Source,
				TargetName: t.Name,
				Status:     e.cellStatus(sk, t.Name),
			})
		}
	}
	return out
}

func (e *Engine) cellStatus(sk AvailableSkill, target string) domain.Status {
	in, ok := e.Manifest.Find(target, sk.Name)
	if !ok || in.SourceName != sk.Source {
		// Either not installed, or installed from a different Source — from
		// this row's perspective it is not installed.
		return domain.StatusNotInstalled
	}
	if in.Fingerprint == sk.Fingerprint {
		return domain.StatusUpToDate
	}
	return domain.StatusUpdateAvailable
}

// Collision is an Install that would write over a folder already present at the
// Target but not tracked in the Manifest — placed by hand or another tool. The
// user must confirm before Skillmux overwrites it (ADR 0002).
type Collision struct {
	SkillName  string
	SourceName string
	TargetName string
	Dir        string
}

// Collisions reports, for a Plan, the untracked folders its Install operations
// would overwrite. Only Install ops can collide: reconcile emits Install solely
// when nothing is tracked for that (Target, Skill), so a folder there is
// untracked by definition.
func (e *Engine) Collisions(plan reconcile.Plan) []Collision {
	targets := e.targetPaths()
	var out []Collision
	for _, op := range plan.Operations {
		if op.Kind != reconcile.Install {
			continue
		}
		path, ok := targets[op.TargetName]
		if !ok {
			continue
		}
		if _, tracked := e.Manifest.Find(op.TargetName, op.SkillName); tracked {
			continue
		}
		dir := filepath.Join(path, op.SkillName)
		if _, err := os.Stat(dir); err == nil {
			out = append(out, Collision{
				SkillName:  op.SkillName,
				SourceName: op.SourceName,
				TargetName: op.TargetName,
				Dir:        dir,
			})
		}
	}
	return out
}

// Plan computes the reconcile Plan for a desired selection against the current
// catalog and Manifest.
func (e *Engine) Plan(desired []reconcile.Cell, cat Catalog) reconcile.Plan {
	return reconcile.Reconcile(desired, availableForReconcile(cat), e.Manifest.Installations)
}

// Apply computes the Plan, executes it, and persists the Manifest. It returns
// the per-operation Report; the error is non-nil only if persisting fails.
func (e *Engine) Apply(desired []reconcile.Cell, cat Catalog, opts apply.Options) (apply.Report, error) {
	plan := e.Plan(desired, cat)
	rep := apply.Apply(plan, e.targetPaths(), e.resolved(cat), e.Manifest, opts)
	if err := manifest.Save(e.manifestPath, e.Manifest); err != nil {
		return rep, err
	}
	return rep, nil
}

func (e *Engine) targetPaths() map[string]string {
	m := map[string]string{}
	for _, t := range e.Config.DomainTargets() {
		// Resolve a leading "~" so a Target path like "~/.claude/skills" lands
		// in the home directory rather than a literal "~" folder under the cwd.
		m[t.Name] = paths.ExpandHome(t.Path)
	}
	return m
}

func (e *Engine) resolved(cat Catalog) map[apply.SkillID]apply.ResolvedSkill {
	m := map[apply.SkillID]apply.ResolvedSkill{}
	for _, sk := range cat.Skills {
		m[apply.SkillID{Source: sk.Source, Skill: sk.Name}] = apply.ResolvedSkill{
			Dir:         sk.Dir,
			Fingerprint: sk.Fingerprint,
		}
	}
	return m
}

func availableForReconcile(cat Catalog) []reconcile.AvailableSkill {
	out := make([]reconcile.AvailableSkill, 0, len(cat.Skills))
	for _, sk := range cat.Skills {
		out = append(out, reconcile.AvailableSkill{Name: sk.Name, Source: sk.Source, Fingerprint: sk.Fingerprint})
	}
	return out
}
