package cache

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "lowercase", in: "WHO scored most points", want: "who scored most points"},
		{name: "punctuation stripped", in: "Who's the 3-point leader? (this season)", want: "whos the 3point leader this season"},
		{name: "collapse whitespace", in: "  Who    scored   points\t\n", want: "who scored points"},
		{name: "symbols dropped", in: "Celtics' record?", want: "celtics record"},
		{name: "already clean", in: "how many points", want: "how many points"},
		{name: "empty", in: "   ", want: ""},
		{name: "digits and hyphen", in: "3-pointers made", want: "3pointers made"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Normalize(c.in); got != c.want {
				t.Fatalf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestKey(t *testing.T) {
	got := Key("2026-11-12", "Who scored the most 3-Pointers?")
	want := "ask:2026-11-12:who scored the most 3pointers"
	if got != want {
		t.Fatalf("Key() = %q, want %q", got, want)
	}
}
