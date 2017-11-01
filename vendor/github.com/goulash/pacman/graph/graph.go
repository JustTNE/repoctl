// Package graph provides dependency resolution for AUR packages.
// It is not very optimized, but it should work (hopefully).
//
// Usage
//
// First, you have to create a graph:
//
//   pkgs, err := aur.ReadAll(list)
//   if err != nil {
//      return err
//   }
//   g, err := graph.NewGraph()
//   if err != nil {
//      return err
//   }
//
// Once you have a graph, you can then get the ordered dependency list
// with the following function:
//
//   graph.Dependencies(g)
package graph

import (
	"fmt"

	"github.com/gonum/graph"
	"github.com/goulash/pacman"
	"github.com/goulash/pacman/aur"
)

// Node implements graph.Node.
type Node struct {
	id int

	pacman.AnyPackage
}

// ID returns the unique (within the graph) ID of the node.
func (n *Node) ID() int { return n.id }

// IsFromAUR returns whether the node comes from AUR.
func (n *Node) IsFromAUR() bool {
	_, ok := n.AnyPackage.(*aur.Package)
	return ok
}

// AllDepends returns a (newly created) string slice of the installation
// and make dependencies of this package.
func (n *Node) AllDepends() []string {
	deps := make([]string, 0, n.NumAllDepends())
	deps = append(deps, n.PkgDepends()...)
	deps = append(deps, n.PkgMakeDepends()...)
	return deps
}

// NumDepends returns the number of make and installation dependencies the package has.
func (n *Node) NumAllDepends() int {
	return len(n.PkgDepends()) + len(n.PkgMakeDepends())
}

func (n *Node) String() string {
	return n.PkgName()
}

// Edge implements the graph.Edge interface.
type Edge struct {
	from *Node
	to   *Node
}

// From returns the node that has the dependency.
func (e *Edge) From() graph.Node { return e.from }

// To returns the depdency that the from node has.
func (e *Edge) To() graph.Node { return e.to }

// Weight returns zero, because depdencies are not weighted.
func (e *Edge) Weight() float64 { return 0.0 }

// IsFromAUR returns true if the dependency needs to be fetched from AUR.
func (e *Edge) IsFromAUR() bool { return e.to.IsFromAUR() }

func (e *Edge) String() string { return fmt.Sprintf("%s -> %s", e.from, e.to) }

// Graph implements graph.Graph.
type Graph struct {
	names     map[string]*Node
	nodes     []graph.Node
	nodeIDs   map[int]graph.Node
	edgesFrom map[int][]graph.Node
	edgesTo   map[int][]graph.Node
	edges     map[int]map[int]graph.Edge
	nextID    int
}

// NewGraph returns a new graph.
func NewGraph() *Graph {
	return &Graph{
		names:     make(map[string]*Node),
		nodes:     make([]graph.Node, 0),
		nodeIDs:   make(map[int]graph.Node),
		edgesFrom: make(map[int][]graph.Node),
		edgesTo:   make(map[int][]graph.Node),
		edges:     make(map[int]map[int]graph.Edge),
		nextID:    0,
	}
}

// Has returns whether the node exists within the graph.
func (g *Graph) Has(n graph.Node) bool {
	_, ok := g.nodeIDs[n.ID()]
	return ok
}

// HasName returns whether the package with the given name exists within the
// graph.
func (g *Graph) HasName(name string) bool {
	_, ok := g.names[name]
	return ok
}

// NodeWithName returns the node with the given name, or nil.
func (g *Graph) NodeWithName(name string) *Node {
	return g.names[name]
}

// Nodes returns all the nodes in the graph.
func (g *Graph) Nodes() []graph.Node {
	return g.nodes
}

// From returns all nodes that can be reached directly from the given node.
func (g *Graph) From(v graph.Node) []graph.Node {
	return g.edgesFrom[v.ID()]
}

// To returns all nodes that can reach directly to the given node.
func (g *Graph) To(v graph.Node) []graph.Node {
	return g.edgesTo[v.ID()]
}

// HasEdgeBetween returns whether an edge exists between nodes u and v
// without considering direction.
func (g *Graph) HasEdgeBetween(u, v graph.Node) bool {
	return g.HasEdgeFromTo(u, v) || g.HasEdgeFromTo(v, u)
}

// HasEdgeFromTo returns whether an edge exists in the graph from u to v.
func (g *Graph) HasEdgeFromTo(u, v graph.Node) bool {
	for _, n := range g.edgesFrom[u.ID()] {
		if n == v {
			return true
		}
	}
	return false
}

// Edge returns the edge from u to v if such an edge exists and nil
// otherwise. The node v must be directly reachable from u as defined
// by the From method.
func (g *Graph) Edge(u, v graph.Node) graph.Edge {
	return g.edges[u.ID()][v.ID()]
}

// NewNodeID returns a unique ID for a new node.
func (g *Graph) NewNodeID() int {
	g.nextID++
	return g.nextID
}

// NewNode returns a new node.
func (g *Graph) NewNode(pkg pacman.AnyPackage) *Node {
	return &Node{
		id:         g.NewNodeID(),
		AnyPackage: pkg,
	}
}

// AddNode adds the node and initializes data structures but does nothing else.
func (g *Graph) AddNode(v graph.Node) {
	// Checking preconditions:
	n, ok := v.(*Node)
	if !ok {
		panic("only accept our own nodes")
	}
	if g.HasName(n.PkgName()) {
		panic("package name already in graph")
	}
	if g.Has(v) {
		panic("node id already here")
	}

	g.names[n.PkgName()] = n
	g.nodes = append(g.nodes, n)
	id := n.ID()
	g.nodeIDs[id] = n
	g.edgesFrom[id] = make([]graph.Node, 0, n.NumAllDepends())
	g.edgesTo[id] = make([]graph.Node, 0)
	g.edges[id] = make(map[int]graph.Edge)
}

// AddEdgeFromTo adds an edge betwewen the two nodes.
func (g *Graph) AddEdgeFromTo(u, v graph.Node) {
	uid, vid := u.ID(), v.ID()
	g.edges[uid][vid] = &Edge{from: u.(*Node), to: v.(*Node)}
	g.edgesFrom[uid] = append(g.edgesFrom[uid], u)
	g.edgesTo[vid] = append(g.edgesTo[vid], v)
}
