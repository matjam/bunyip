package grid_test

import (
	"fmt"

	"github.com/matjam/bunyip/grid"
)

// A five-by-three map with a wall in the middle column, open at the top.
var walls = []string{
	".....",
	"..#..",
	"..#..",
}

func cost(from, to grid.Point) float32 {
	if walls[to.Y][to.X] == '#' {
		return grid.Blocked
	}
	return 1
}

func ExampleAStar() {
	path := grid.AStar(5, 3, grid.Point{X: 0, Y: 2}, grid.Point{X: 4, Y: 2}, false, cost)
	fmt.Println(len(path)-1, "steps:", path)
	// Output:
	// 8 steps: [{0 2} {1 2} {1 1} {1 0} {2 0} {3 0} {4 0} {4 1} {4 2}]
}

func ExampleDijkstra() {
	// A Dijkstra map gives every cell its distance to the player; a
	// monster walks downhill to chase, or uphill to flee.
	dist := grid.Dijkstra(5, 3, []grid.Point{{X: 4, Y: 2}}, false, cost)
	next, _ := grid.Downhill(dist, grid.Point{X: 0, Y: 2}, false)
	fmt.Println(dist.At(0, 2), next)
	// Output:
	// 8 {1 2}
}

func ExampleFOV() {
	inMap := func(p grid.Point) bool { return p.X >= 0 && p.Y >= 0 && p.X < 5 && p.Y < 3 }
	// Cells off the map count as opaque so light stops at the edge.
	opaque := func(p grid.Point) bool { return !inMap(p) || walls[p.Y][p.X] == '#' }
	var seen []grid.Point
	grid.FOV(grid.Point{X: 0, Y: 2}, 10, opaque, func(p grid.Point) {
		if inMap(p) {
			seen = append(seen, p)
		}
	})
	// The wall's near face is visible; the cells behind it are not.
	fmt.Println(len(seen), "cells visible of 15")
	// Output:
	// 10 cells visible of 15
}
