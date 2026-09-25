package ecs

import (
	"fmt"
	"strings"
	"testing"
)

// TestSceneDuplicateNames checks AddEntity and AddPrefab refuse a name
// the scene already has, however the earlier entity got there: through
// AddEntity, through an append to Entities, or in a slice that replaced
// Entities, and that a name freed by a rename in place can be used again.
func TestSceneDuplicateNames(t *testing.T) {
	dup := func(t *testing.T, s *Scene, name string) {
		t.Helper()
		_, err := s.AddEntity(name, pos{})
		if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("two entities are named %q", name)) {
			t.Fatalf("AddEntity(%q) = %v, want the duplicate-name error", name, err)
		}
		if _, err := s.AddPrefab(name, "p"); err == nil {
			t.Fatalf("AddPrefab(%q) accepted a duplicate", name)
		}
	}
	s := NewScene("names")
	for i := range 100 {
		name := ""
		if i%3 != 0 {
			name = fmt.Sprintf("e%d", i)
		}
		if _, err := s.AddEntity(name, pos{}); err != nil {
			t.Fatal(err)
		}
	}
	dup(t, s, "e1")
	dup(t, s, "e98")
	if n := len(s.Entities); n != 100 {
		t.Fatalf("a refused add left %d entities, want 100", n)
	}
	// Unnamed entities never collide.
	if _, err := s.AddEntity("", pos{}); err != nil {
		t.Fatal(err)
	}

	s.Entities = append(s.Entities, SceneEntity{Name: "direct"})
	dup(t, s, "direct")

	s.Entities = []SceneEntity{{Name: "fresh"}}
	dup(t, s, "fresh")
	if _, err := s.AddEntity("e1", pos{}); err != nil {
		t.Fatalf("a name from the replaced slice is still taken: %v", err)
	}

	s.Entities[0].Name = "renamed"
	if _, err := s.AddEntity("fresh", pos{}); err != nil {
		t.Fatalf("a name freed by a rename is still taken: %v", err)
	}
	dup(t, s, "fresh")

	// A copy that adds to the index the original shares does not make
	// the original refuse a name it does not have.
	c := *s
	if _, err := c.AddEntity("copy-only", pos{}); err != nil {
		t.Fatal(err)
	}
	s.Entities = s.Entities[:len(s.Entities):len(s.Entities)]
	if _, err := s.AddEntity("copy-only", pos{}); err != nil {
		t.Fatalf("the original refused a name only its copy has: %v", err)
	}
}
