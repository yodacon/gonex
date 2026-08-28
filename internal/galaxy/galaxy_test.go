package galaxy

import "testing"

func mustLoad(t *testing.T) *Galaxy {
	t.Helper()
	g, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestLoadShape(t *testing.T) {
	g := mustLoad(t)
	if len(g.Systems) < 100 || len(g.Stellars) < 100 {
		t.Fatalf("got %d systems / %d stellars; want the full joined map",
			len(g.Systems), len(g.Stellars))
	}
	conex := g.Systems[128]
	if conex == nil || conex.Name != "ConEx" {
		t.Fatalf("system 128 = %+v; want ConEx (the overridden Levo)", conex)
	}
	if st := g.Stellars[133]; st == nil || st.Name != "ConEx" || st.System != 128 {
		t.Fatalf("stellar 133 = %+v; want ConEx station in system 128", st)
	}
	if st := g.Stellars[128]; st == nil || st.Name != "Earth" {
		t.Fatalf("stellar 128 = %+v; want Earth", st)
	}
}

// The Marks Logging delivery leg: ConEx home to Polaris (Northstar) runs
// via Rigel and Matar on the recovered links.
func TestRouteConExToPolaris(t *testing.T) {
	g := mustLoad(t)
	route := g.Route(128, 144)
	want := []int{128, 136, 151, 144}
	if len(route) != len(want) {
		t.Fatalf("route = %v; want %v", route, want)
	}
	for i := range want {
		if route[i] != want[i] {
			t.Fatalf("route = %v; want %v", route, want)
		}
	}
}

// Exeon's 1997 self-link must not survive into the game data.
func TestExeonSelfLinkFiltered(t *testing.T) {
	g := mustLoad(t)
	for _, l := range g.Systems[236].Links {
		if l == 236 {
			t.Fatalf("Exeon still links to itself: %v", g.Systems[236].Links)
		}
	}
}

func TestRouteUnreachableIsNil(t *testing.T) {
	g := mustLoad(t)
	if r := g.Route(128, 99999); r != nil {
		t.Fatalf("route to nowhere = %v", r)
	}
}
