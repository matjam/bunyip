package gltf

import "fmt"

// validateHierarchy enforces glTF's disjoint strict trees before any
// resources are decoded or nodes are traversed. Each node and reference
// is checked a constant number of times, including unreachable nodes.
func (l *loader) validateHierarchy() error {
	parents := make([]int, len(l.j.Nodes))
	for i := range parents {
		parents[i] = -1
	}
	for i, node := range l.j.Nodes {
		for childIndex, child := range node.Children {
			if child < 0 || child >= len(parents) {
				return fmt.Errorf("node %d: child %d references node %d out of range", i, childIndex, child)
			}
			if parents[child] == i {
				return fmt.Errorf("node %d: duplicate child node %d", i, child)
			}
			if parents[child] != -1 {
				return fmt.Errorf("node %d: child node %d already has parent %d", i, child, parents[child])
			}
			parents[child] = i
		}
	}

	// Following the single parent link needs no recursion. A visiting
	// node belongs to this walk; completed walks are marked done, so
	// even a deep chain is visited only once across all starting nodes.
	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]uint8, len(parents))
	for start := range parents {
		if state[start] != unvisited {
			continue
		}
		n := start
		for n != -1 && state[n] == unvisited {
			state[n] = visiting
			n = parents[n]
		}
		if n != -1 && state[n] == visiting {
			return fmt.Errorf("node %d: cycle in hierarchy", n)
		}
		for n = start; n != -1 && state[n] == visiting; n = parents[n] {
			state[n] = done
		}
	}

	if l.j.Scene != nil && (*l.j.Scene < 0 || *l.j.Scene >= len(l.j.Scenes)) {
		return fmt.Errorf("default scene %d out of range", *l.j.Scene)
	}
	// A root may occur in several scenes, but only once in each scene.
	rootScene := make([]int, len(parents))
	for i, scene := range l.j.Scenes {
		for rootIndex, root := range scene.Nodes {
			if root < 0 || root >= len(parents) {
				return fmt.Errorf("scene %d: root %d references node %d out of range", i, rootIndex, root)
			}
			if parents[root] != -1 {
				return fmt.Errorf("scene %d: root node %d has parent %d", i, root, parents[root])
			}
			if rootScene[root] == i+1 {
				return fmt.Errorf("scene %d: duplicate root node %d", i, root)
			}
			rootScene[root] = i + 1
		}
	}
	return nil
}
