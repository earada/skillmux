// Package engine wires the core together for the TUI: it loads Config and
// Manifest, refreshes the catalog of available Skills from the configured
// Sources (fetch + scan + fingerprint), computes the per-cell Status, and turns
// a desired Skill×Target selection into a Plan it can Apply and persist.
package engine

import (
	"os"
	"path/filepath"
	"sync"
	"time"

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

	// mu guards the skill-view coordination below, which a background Refresh
	// (running in its own goroutine) reads while the UI loop writes it.
	mu sync.Mutex
	// viewedSource is the Source whose files are currently being explored; a
	// concurrent Refresh updates its objects but defers the working-tree
	// checkout so the explorer never reads a half-rewritten tree.
	viewedSource string
	// deferred records Sources whose checkout a Refresh skipped because they
	// were being viewed, so the view's close can trigger a catch-up Refresh.
	deferred map[string]bool
}

// New builds an Engine from already-loaded state. Used directly in tests.
// configPath may be empty when the caller will not mutate the Config.
func New(cfg *config.Config, man *manifest.Manifest, fetcher *fetch.Fetcher, configPath, manifestPath string) *Engine {
	return &Engine{
		Config: cfg, Manifest: man, Fetcher: fetcher,
		configPath: configPath, manifestPath: manifestPath,
		deferred: map[string]bool{},
	}
}

// BeginView marks source as being viewed: a concurrent Refresh will update its
// git objects but defer the working-tree checkout. Pair every BeginView with an
// EndView when the view closes.
func (e *Engine) BeginView(source string) {
	e.mu.Lock()
	e.viewedSource = source
	e.mu.Unlock()
}

// EndView clears the viewed Source and reports whether a Refresh deferred its
// checkout while it was open. When true, the caller should Refresh again to
// apply the now-safe checkout.
func (e *Engine) EndView() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	deferred := e.deferred[e.viewedSource]
	delete(e.deferred, e.viewedSource)
	e.viewedSource = ""
	return deferred
}

// ViewedSource reports the Source currently marked as being viewed (empty when
// none). Exposed for the UI and its tests.
func (e *Engine) ViewedSource() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.viewedSource
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
	// Group is the folder hierarchy the Skill sits under within its Source
	// (empty for a root-level Skill); shown as a dimmed prefix in the matrix.
	Group string
	// Deprecated and DeprecationReason mirror the SKILL.md frontmatter so the
	// matrix can flag a Skill its author has retired.
	Deprecated        bool
	DeprecationReason string
	// Refs are the names of other catalog Skills this Skill references in its
	// files (via a /<name> token or a ../<name>/ path), excluding itself.
	// Whether each is a Dependency or a Suggestion is decided against Config by
	// the SkillGraph; here they are just the raw resolved references.
	Refs []string `json:"refs,omitempty"`
}

// Catalog is the result of a Refresh: the available Skills, the Revision of each
// GitHub Source's clone, and any per-Source errors encountered (Refresh is
// best-effort across Sources).
type Catalog struct {
	Skills       []AvailableSkill
	Revisions    map[string]domain.Revision // keyed by Source name; GitHub Sources only
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
	cat := Catalog{
		Revisions:    map[string]domain.Revision{},
		SourceErrors: map[string]error{},
	}
	for _, src := range e.Config.DomainSources() {
		e.mu.Lock()
		viewing := src.Name == e.viewedSource
		e.mu.Unlock()

		var root string
		var err error
		if viewing {
			// A skill view is open on this Source: refresh its objects but leave
			// the working tree alone, and remember to check it out once the view
			// closes.
			root, err = e.Fetcher.FetchObjectsOnly(src)
			e.mu.Lock()
			e.deferred[src.Name] = true
			e.mu.Unlock()
		} else {
			root, err = e.Fetcher.Fetch(src)
		}
		if err != nil {
			cat.SourceErrors[src.Name] = err
			continue
		}
		if rev, ok := e.Fetcher.Revision(src); ok {
			rev.FetchedAt = time.Now()
			cat.Revisions[src.Name] = rev
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
				Name:              sk.Name,
				Source:            sk.SourceName,
				Description:       sk.Description,
				Dir:               dir,
				Fingerprint:       fp,
				Group:             sk.Group,
				Deprecated:        sk.Deprecated,
				DeprecationReason: sk.DeprecationReason,
			})
		}
	}
	resolveRefs(cat.Skills)
	e.saveCatalog(cat)
	return cat
}

// resolveRefs fills each Skill's Refs with the names of other catalog Skills it
// references. It runs after every Source is scanned, since a reference resolves
// against the whole catalog's names, not just the referencing Skill's Source. A
// per-Skill scan error is ignored: a missing reference is never worse than the
// status quo (no Dependency surfaced).
func resolveRefs(skills []AvailableSkill) {
	known := make(map[string]bool, len(skills))
	for _, sk := range skills {
		known[sk.Name] = true
	}
	for i := range skills {
		refs, err := source.References(skills[i].Dir, known)
		if err != nil {
			continue
		}
		out := refs[:0]
		for _, r := range refs {
			if r != skills[i].Name { // a Skill does not depend on itself
				out = append(out, r)
			}
		}
		skills[i].Refs = out
	}
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
