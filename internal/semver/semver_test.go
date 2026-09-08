package semver

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want []int
	}{
		{"1.2.3", []int{1, 2, 3}},
		{"v1.2.3", []int{1, 2, 3}},
		{"0.0.0", []int{0, 0, 0}},
		{"1.2", nil},
		{"1.2.3.4", nil},
		{"1.2.x", nil},
		{"", nil},
		{"1.2.3-rc1", nil},
	}
	for _, c := range cases {
		got := Parse(c.in)
		if !intSliceEqual(got, c.want) {
			t.Errorf("Parse(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.0.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"v1.0.0", "1.0.0", 0},
		{"bogus", "1.0.0", -1},
		{"1.0.0", "bogus", 1},
		{"bogus", "also-bogus", 0},
	}
	for _, c := range cases {
		got := Compare(c.a, c.b)
		if got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCompareSymmetry(t *testing.T) {
	pairs := [][2]string{
		{"1.0.0", "2.0.0"},
		{"1.2.3", "1.2.4"},
		{"0.0.1", "0.1.0"},
	}
	for _, p := range pairs {
		a, b := Compare(p[0], p[1]), Compare(p[1], p[0])
		if a != -b {
			t.Errorf("Compare(%q,%q)=%d, Compare(%q,%q)=%d; not symmetric", p[0], p[1], a, p[1], p[0], b)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, candidate string
		want               bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.1", "1.0.0", false},
		{"1.0.0", "1.0.0", false},
		{"v0.2.11", "v0.2.12", true},
		{"0.2.12", "v0.2.12", false},
		{"bogus", "1.0.0", false},
		{"1.0.0", "bogus", false},
	}
	for _, c := range cases {
		got := IsNewer(c.current, c.candidate)
		if got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.candidate, got, c.want)
		}
	}
}
