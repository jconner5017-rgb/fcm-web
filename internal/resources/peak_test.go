package resources

import "testing"

// One busy hour on an otherwise quiet day shows yellow only past 95%, not red.
func TestPeakGauge(t *testing.T) {
	g := Gauge{Used: 70, PeakUsed: 202, Limit: 268, Peak: true}
	g.fill()
	if g.Level != "ok" || int(g.PeakPercent) != 75 {
		t.Fatalf("quiet day with a 75%% hour: %+v", g)
	}
	g = Gauge{Used: 70, PeakUsed: 260, Limit: 268, Peak: true}
	g.fill()
	if g.Level != "warn" {
		t.Fatalf("a 97%% hour: %+v", g)
	}
	g = Gauge{Used: 240, PeakUsed: 250, Limit: 268, Peak: true}
	g.fill()
	if g.Level != "high" {
		t.Fatalf("busy all day: %+v", g)
	}
}
