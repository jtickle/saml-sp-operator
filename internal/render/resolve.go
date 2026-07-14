package render

import (
	"cmp"
	"slices"
)

// hostPathKey is the RequestMap collision key: an AppBinding "wins" a
// (Hostname, Path) pair, or loses it to whichever binding claims the key
// first in ranked order.
type hostPathKey struct {
	Hostname string
	Path     string
}

// Resolve computes the deterministic RequestMap winner for every colliding
// (Hostname, Path) key across bindings (D-06). It is exported and callable
// independently so the AppIntegration controller (APP-04) can compute its
// own Conflict condition over its sibling list using the exact same logic
// the SPInstance controller uses — this decomposition IS the "both
// controllers can never disagree" guarantee: they run the same Resolve.
//
// Resolve never ranges a Go map to build ordered output. Winners and
// Conflicts are always emitted from rankOrder's deterministic slice, so
// RENDER-09's config-hash stability never depends on Go map iteration order
// (see this plan's threat_model T-01-02).
func Resolve(bindings []AppBinding) (Resolution, error) {
	ranked := rankOrder(bindings)

	claimed := make(map[hostPathKey]AppBinding, len(ranked))

	var winners []AppBinding
	var conflicts []Conflict

	for _, b := range ranked {
		key := hostPathKey{Hostname: b.Hostname, Path: b.Path}

		winner, alreadyClaimed := claimed[key]
		if !alreadyClaimed {
			claimed[key] = b
			winners = append(winners, b)
			continue
		}

		conflicts = append(conflicts, Conflict{
			Winner:         winner,
			LoserNamespace: b.Namespace,
			LoserName:      b.Name,
			LoserUID:       b.UID,
			Hostname:       b.Hostname,
			Path:           b.Path,
		})
	}

	return Resolution{Winners: winners, Conflicts: conflicts}, nil
}

// rankOrder returns a new slice (the input is never mutated), sorted by the
// deterministic sort key (priority desc, createdAt asc, UID asc) (D-07).
//
// slices.SortFunc — not slices.SortStableFunc — is the correct choice here:
// the UID tiebreak makes the comparator a strict total order (no two
// bindings with distinct UIDs can ever compare equal), so sort stability is
// not a correctness requirement. Do not "fix" this to SortStableFunc under a
// mistaken belief it's needed — it isn't, and it would only mask a bug if
// the comparator were ever accidentally weakened to allow ties.
func rankOrder(bindings []AppBinding) []AppBinding {
	out := slices.Clone(bindings)
	slices.SortFunc(out, func(a, b AppBinding) int {
		return cmp.Or(
			cmp.Compare(b.Priority, a.Priority),           // desc: higher priority first
			cmp.Compare(a.CreatedAtUnix, b.CreatedAtUnix), // asc: older first
			cmp.Compare(a.UID, b.UID),                     // asc: final tiebreak
		)
	})
	return out
}
