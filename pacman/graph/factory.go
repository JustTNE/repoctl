package graph

import (
	"os"

	"github.com/goulash/errs"
	"github.com/goulash/pacman"
	"github.com/goulash/pacman/aur"
)

// A Factory creates a dependency graph given a set of packages.
//
// It can perform AUR calls to resolve dependencies and it can
// truncate dependencies so that they are not resolved beyond
// packages available in repositories. This reduces the
// dependency list
type Factory struct {
	local map[string]*pacman.Package
	sync  map[string]*pacman.Package

	// Options
	skipInstalled bool
	truncate      bool
	noUnknown     bool
	depFunc       func(pacman.AnyPackage) []string

	// Statistics
	aurCalls int
}

// NewFactory returns a new dependency graph, ignoring repositories given in `ignore`.
//
// Note that it makes a difference between dependencies that are in AUR, and those
// availabe in the repositories. Ignoring repositories effectively "demotes" any
// packages available in them back to AUR.
//
// Any package that is in the dependency graph that is not from AUR is treated
// as a leaf in the graph, since we assume that pacman can resolve those
// dependencies.
func NewFactory(ignoreRepos ...string) (*Factory, error) {
	f := Factory{
		skipInstalled: false,
		truncate:      false,
		noUnknown:     false,
		depFunc: func(p pacman.AnyPackage) []string {
			deps := make([]string, 0, len(p.PkgDepends())+len(p.PkgMakeDepends()))
			deps = append(deps, p.PkgDepends()...)
			deps = append(deps, p.PkgMakeDepends()...)
			return deps
		},
	}

	// Read local database
	lpkgs, err := pacman.ReadLocalDatabase(errs.Print(os.Stderr))
	if err != nil {
		return nil, err
	}
	f.local = lpkgs.ToMap()

	// Read available packages
	var pkgs pacman.Packages
	if len(ignoreRepos) == 0 {
		pkgs, err = pacman.ReadAllSyncDatabases()
		if err != nil {
			return nil, err
		}
	} else {
		enabled, err := pacman.EnabledRepositories()
		if err != nil {
			return nil, err
		}
	nextRepo:
		for _, repo := range enabled {
			for _, ig := range ignoreRepos {
				if repo == ig {
					continue nextRepo
				}
			}

			rpkgs, err := pacman.ReadSyncDatabase(repo)
			if err != nil {
				return nil, err
			}
			pkgs = append(pkgs, rpkgs...)
		}
	}
	f.sync = pkgs.ToMap()
	return &f, nil
}

// SetSkipInstalled controls whether installed packages are
// disregarded from the dependency tree.
func (f *Factory) SetSkipInstalled(yes bool) {
	f.skipInstalled = yes
}

// SetTruncate controls whether packages available in a repository
// are not further investigated.
func (f *Factory) SetTruncate(yes bool) {
	f.truncate = yes
}

// SetNoUnknown controls whether unknown packages (i.e., not on AUR) are
// injected into the graph. If this is set to true, NewGraph will fail if any
// unknown packages are found.
func (f *Factory) SetNoUnknown(yes bool) {
	f.noUnknown = yes
}

// SetDependencyFunc controls which dependency list is used.
// By default, make and install dependencies are included.
func (f *Factory) SetDependencyFunc(fn func(pacman.AnyPackage) []string) {
	f.depFunc = fn
}

// NumRequestsAUR returns the number of requests made to AUR.
func (f *Factory) NumRequestsAUR() int {
	return f.aurCalls
}

// NewGraph returns a dependency graph of the given AUR packages.
// Extra packages may be pulled into the graph to properly build
// the dependency graph.
func (f *Factory) NewGraph(pkgs aur.Packages) (*Graph, error) {
	g := NewGraph()

	lst := make([]*Node, 0, len(pkgs))
	for _, p := range pkgs {
		v := g.NewNode(p)
		lst = append(lst, v)
		g.AddNode(v)
	}

	// As long as we have new packages to process, continue.
	for len(lst) == 0 {
		new := make([]*Node, 0)
		unavailable := make(map[string]bool, 0)
		pending := make(map[string][]*Node)

		// For each package to add edges for:
		for _, v := range lst {
			for _, d := range f.depFunc(v.AnyPackage) {
				if g.HasName(d) {
					// Dependency already in the graph, so add the edge:
					u := g.NodeWithName(d)
					g.AddEdgeFromTo(v, u)
					continue
				}

				if f.skipInstalled {
					if _, ok := f.local[d]; ok {
						continue
					}
				}

				if p, ok := f.sync[d]; ok {
					u := g.NewNode(p)
					if !f.truncate {
						// Process this package for dependencies
						new = append(new, u)
					}
					g.AddEdgeFromTo(v, u)
					continue
				}

				// If we got this fas, then d is an unknown dependency,
				// and therefore must be in AUR (otherwise we're in trouble).
				// We haven't added an edge for this yet, so we need to remember that.
				unavailable[d] = true
				pending[d] = append(pending[d], v)
			}
		}

		// This may be called for AUR or unknown packages
		addFetchedPkg := func(p pacman.AnyPackage) {
			u := g.NewNode(p)
			new = append(new, u)
			g.AddNode(u)
			for _, v := range pending[u.PkgName()] {
				g.AddEdgeFromTo(v, u)
			}
		}

		// Get all unavailable packages from AUR:
		fromAUR := make([]string, len(unavailable))
		for k := range unavailable {
			fromAUR = append(fromAUR, k)
		}
		f.aurCalls++
		pkgs, err := aur.ReadAll(fromAUR)

		// Add the AUR packages to the graph and to the list of new packages.
		// Also add the edges that we remembered.
		if err != nil {
			if !f.noUnknown && aur.IsNotFound(err) {
				// the unknown packages are not found in AUR...
				for _, p := range err.(*aur.NotFoundError).Names {
					addFetchedPkg(&pacman.Package{
						Name:        p,
						Origin:      pacman.UnknownOrigin,
						Depends:     make([]string, 0),
						MakeDepends: make([]string, 0),
					})
				}
			} else {
				return nil, err
			}
		}
		for _, p := range pkgs {
			addFetchedPkg(p)
		}

		lst = new
	}

	return g, nil
}
