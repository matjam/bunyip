package save

import "testing"

func TestLoadIndependentDefaults(t *testing.T) {
	for _, present := range []bool{false, true} {
		s, err := OpenAt(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if present {
			if err := s.Write("settings", map[string]any{}); err != nil {
				t.Fatal(err)
			}
		}
		type config struct{ Values map[string][]int }
		defaults := config{Values: map[string][]int{"key": {1, 2}}}
		got, err := s.Load("settings", defaults)
		if err != nil {
			t.Fatal(err)
		}
		got.Values["key"][0] = 99
		got.Values["other"] = []int{3}
		if defaults.Values["key"][0] != 1 || len(defaults.Values) != 1 {
			t.Fatalf("present=%v: defaults mutated: %#v", present, defaults)
		}
	}
}

func TestLoadRejectsInvalidDefaults(t *testing.T) {
	s, err := OpenAt(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("settings", make(chan int)); err == nil {
		t.Fatal("unsupported defaults accepted")
	}
}
