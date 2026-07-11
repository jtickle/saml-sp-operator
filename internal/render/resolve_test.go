package render

import (
	"math/rand"
	"reflect"
	"testing"
)

// TestResolveDeterminism exercises Resolve (RENDER-06 / D-06 / D-07). The
// same-second-CreatedAtUnix + differing-UID case is the load-bearing one:
// metav1.Time is second-granular, so two AppIntegrations created in the same
// wall-clock second tie on CreatedAtUnix and MUST fall through to the UID
// tiebreak for the whole system to stay deterministic (ROADMAP crit 2).
func TestResolveDeterminism(t *testing.T) {
	t.Run("shuffled input yields byte-identical Winners every run", func(t *testing.T) {
		base := SampleAppBindings()

		first, err := Resolve(base)
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}

		for i := 0; i < 100; i++ {
			shuffled := append([]AppBinding(nil), base...)
			rand.Shuffle(len(shuffled), func(i, j int) {
				shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
			})

			got, err := Resolve(shuffled)
			if err != nil {
				t.Fatalf("Resolve returned unexpected error on shuffle %d: %v", i, err)
			}
			if !reflect.DeepEqual(got.Winners, first.Winners) {
				t.Fatalf("Resolve shuffle %d Winners = %+v, want identical to unshuffled %+v", i, got.Winners, first.Winners)
			}
			if !reflect.DeepEqual(got.Conflicts, first.Conflicts) {
				t.Fatalf("Resolve shuffle %d Conflicts = %+v, want identical to unshuffled %+v", i, got.Conflicts, first.Conflicts)
			}
		}
	})

	t.Run("same-second CreatedAtUnix falls through to the lower UID", func(t *testing.T) {
		lowerUID := AppBinding{
			Namespace: "ns-a", Name: "app-a", UID: "aaaa",
			Hostname: "collide.example.com", Path: "/",
			Priority: 0, CreatedAtUnix: 1_700_000_000,
		}
		higherUID := AppBinding{
			Namespace: "ns-z", Name: "app-z", UID: "zzzz",
			Hostname: "collide.example.com", Path: "/",
			Priority: 0, CreatedAtUnix: 1_700_000_000,
		}

		got, err := Resolve([]AppBinding{higherUID, lowerUID})
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}

		if len(got.Winners) != 1 || got.Winners[0].UID != "aaaa" {
			t.Fatalf("Resolve Winners = %+v, want exactly one winner with UID %q", got.Winners, "aaaa")
		}
		if len(got.Conflicts) != 1 {
			t.Fatalf("Resolve Conflicts = %+v, want exactly one conflict", got.Conflicts)
		}
		conflict := got.Conflicts[0]
		if conflict.LoserUID != "zzzz" || conflict.Winner.UID != "aaaa" {
			t.Fatalf("Resolve Conflicts[0] = %+v, want LoserUID %q and Winner.UID %q", conflict, "zzzz", "aaaa")
		}
	})

	t.Run("higher priority beats older CreatedAtUnix", func(t *testing.T) {
		olderLowPriority := AppBinding{
			Namespace: "ns-old", Name: "app-old", UID: "old-uid",
			Hostname: "collide.example.com", Path: "/",
			Priority: 0, CreatedAtUnix: 100,
		}
		newerHighPriority := AppBinding{
			Namespace: "ns-new", Name: "app-new", UID: "new-uid",
			Hostname: "collide.example.com", Path: "/",
			Priority: 5, CreatedAtUnix: 200,
		}

		got, err := Resolve([]AppBinding{olderLowPriority, newerHighPriority})
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}

		if len(got.Winners) != 1 || got.Winners[0].UID != "new-uid" {
			t.Fatalf("Resolve Winners = %+v, want the higher-priority binding (UID %q) to win despite being newer", got.Winners, "new-uid")
		}
		if len(got.Conflicts) != 1 || got.Conflicts[0].LoserUID != "old-uid" {
			t.Fatalf("Resolve Conflicts = %+v, want the older lower-priority binding (UID %q) as the loser", got.Conflicts, "old-uid")
		}
	})

	t.Run("non-colliding bindings all win, Conflicts is empty", func(t *testing.T) {
		solo1 := AppBinding{UID: "s1", Hostname: "one.example.com", Path: "/", CreatedAtUnix: 1}
		solo2 := AppBinding{UID: "s2", Hostname: "two.example.com", Path: "/", CreatedAtUnix: 2}
		solo3 := AppBinding{UID: "s3", Hostname: "one.example.com", Path: "/other", CreatedAtUnix: 3}

		got, err := Resolve([]AppBinding{solo1, solo2, solo3})
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}

		if len(got.Winners) != 3 {
			t.Fatalf("Resolve Winners = %+v, want all 3 non-colliding bindings to win", got.Winners)
		}
		if len(got.Conflicts) != 0 {
			t.Fatalf("Resolve Conflicts = %+v, want empty for non-colliding bindings", got.Conflicts)
		}
	})

	t.Run("SampleAppBindings fixture: priority wins the 3-way collision, solos untouched", func(t *testing.T) {
		got, err := Resolve(SampleAppBindings())
		if err != nil {
			t.Fatalf("Resolve returned unexpected error: %v", err)
		}

		winnerUIDs := make(map[string]bool, len(got.Winners))
		for _, w := range got.Winners {
			winnerUIDs[w.UID] = true
		}

		// team-c (UID cccc-0003) has the highest Priority in the collision
		// group despite being the oldest — it must win over team-a and team-b.
		if !winnerUIDs["cccc-0003"] {
			t.Fatalf("Resolve Winners = %+v, want the highest-priority colliding binding (cccc-0003) among winners", got.Winners)
		}
		if winnerUIDs["aaaa-0001"] || winnerUIDs["bbbb-0002"] {
			t.Fatalf("Resolve Winners = %+v, want aaaa-0001 and bbbb-0002 excluded (they lost to cccc-0003)", got.Winners)
		}
		if !winnerUIDs["dddd-0004"] || !winnerUIDs["eeee-0005"] {
			t.Fatalf("Resolve Winners = %+v, want the non-colliding bindings (dddd-0004, eeee-0005) to win unconditionally", got.Winners)
		}
		if len(got.Winners) != 3 {
			t.Fatalf("Resolve Winners = %+v, want exactly 3 winners", got.Winners)
		}

		if len(got.Conflicts) != 2 {
			t.Fatalf("Resolve Conflicts = %+v, want exactly 2 conflicts (aaaa-0001 and bbbb-0002 lost to cccc-0003)", got.Conflicts)
		}
		for _, c := range got.Conflicts {
			if c.Winner.UID != "cccc-0003" {
				t.Fatalf("Resolve Conflicts entry = %+v, want Winner.UID %q", c, "cccc-0003")
			}
		}
	})
}
