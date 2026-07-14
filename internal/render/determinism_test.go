package render

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"
)

// determinism_test.go is the end-to-end tie between confighash.go's Hash
// (plan 02) and the real renderers (plans 03-05), proving RENDER-09's
// reorder-stability property (ROADMAP crit 4) holds across the whole
// pipeline: Resolve never ranges a Go map to build its ordered output
// (resolve.go), and none of the three renderers range a map either, so no
// nondeterminism can leak from []AppBinding input order into the rendered
// bytes or the config hash a Phase 2 pod-template annotation gates a roll
// on.

// renderAll runs the full pipeline (Resolve -> RenderShibboleth2 ->
// RenderNginxConf -> RenderAttributeMap -> Hash) over cfg/bindings/attrs,
// returning the three rendered files (in the fixed order Hash's own doc
// comment specifies: shibboleth2.xml, nginx.conf, attribute-map.xml) and
// their combined hash.
func renderAll(t *testing.T, cfg SPConfig, bindings []AppBinding, attrs []AttributeMapping) ([]ConfigFile, string) {
	t.Helper()

	res, err := Resolve(bindings)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	shib, err := RenderShibboleth2(cfg, res.Winners)
	if err != nil {
		t.Fatalf("RenderShibboleth2: %v", err)
	}

	nginx, err := RenderNginxConf(cfg)
	if err != nil {
		t.Fatalf("RenderNginxConf: %v", err)
	}

	amap, err := RenderAttributeMap(attrs)
	if err != nil {
		t.Fatalf("RenderAttributeMap: %v", err)
	}

	files := []ConfigFile{
		{Name: fixtureShibboleth2Name, Bytes: shib},
		{Name: fixtureNginxConfName, Bytes: nginx},
		{Name: shibAttributeMapPath, Bytes: amap},
	}
	return files, Hash(files)
}

// TestConfigHashStability is RENDER-09's end-to-end proof (ROADMAP crit 4):
// rendering the same input twice is byte-identical, shuffling []AppBinding
// input order never perturbs the config hash, and a semantically-meaningful
// field change always does.
func TestConfigHashStability(t *testing.T) {
	cfg := SampleSPConfig()
	bindings := SampleAppBindings()
	attrs := []AttributeMapping{
		{Name: "email", Header: "X-Remote-User"},
		{Name: "uid", Header: "X-Remote-Uid"},
	}

	t.Run("render-twice-byte-identical", func(t *testing.T) {
		files1, hash1 := renderAll(t, cfg, bindings, attrs)
		files2, hash2 := renderAll(t, cfg, bindings, attrs)

		for i := range files1 {
			if !bytes.Equal(files1[i].Bytes, files2[i].Bytes) {
				t.Errorf("%s is not byte-identical across two renders of the same input", files1[i].Name)
			}
		}
		if hash1 != hash2 {
			t.Errorf("config hash not identical across two renders of the same input: %s vs %s", hash1, hash2)
		}
	})

	t.Run("reorder-stable-across-shuffles", func(t *testing.T) {
		_, baseHash := renderAll(t, cfg, bindings, attrs)

		// A fixed seed keeps this test itself deterministic (a flaky
		// adversarial test would defeat its own purpose) while still
		// exercising many distinct orderings of the same underlying set.
		rng := rand.New(rand.NewSource(1))
		const shuffles = 50
		for i := range shuffles {
			shuffled := slices.Clone(bindings)
			rng.Shuffle(len(shuffled), func(a, b int) {
				shuffled[a], shuffled[b] = shuffled[b], shuffled[a]
			})

			_, hash := renderAll(t, cfg, shuffled, attrs)
			if hash != baseHash {
				t.Fatalf("shuffle #%d: config hash changed from %s to %s despite only []AppBinding input order changing — a map range leaked nondeterminism into the pipeline", i, baseHash, hash)
			}
		}
	})

	t.Run("semantic-change-changes-hash", func(t *testing.T) {
		_, baseHash := renderAll(t, cfg, bindings, attrs)

		mutatedAttrs := slices.Clone(attrs)
		mutatedAttrs[0].Name = "changed-attribute-id"
		_, hashAfterAttrChange := renderAll(t, cfg, bindings, mutatedAttrs)
		if hashAfterAttrChange == baseHash {
			t.Error("changing an AttributeMapping.Name (attribute id) did not change the config hash")
		}

		mutatedCfg := cfg
		mutatedCfg.EntityID = "https://changed.example.com/shibboleth"
		_, hashAfterEntityChange := renderAll(t, mutatedCfg, bindings, attrs)
		if hashAfterEntityChange == baseHash {
			t.Error("changing SPConfig.EntityID did not change the config hash")
		}
	})
}
