/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"strings"
	"testing"

	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
)

// newSampleSPInstance returns a hand-built SPInstance with no cluster
// dependency (renderConfig is a pure unit — no envtest, no Docker).
func newSampleSPInstance() *samlv1alpha1.SPInstance {
	return &samlv1alpha1.SPInstance{
		Spec: samlv1alpha1.SPInstanceSpec{
			EntityID: "https://sp.example.org/shibboleth",
			Credentials: samlv1alpha1.SecretReference{
				Name: "sp-credentials",
			},
			IdP: samlv1alpha1.IdPConfig{
				MetadataURL: "https://idp.example.org/metadata",
				EntityID:    "https://idp.example.org/idp/shibboleth",
			},
		},
	}
}

func TestRenderConfig(t *testing.T) {
	sp := newSampleSPInstance()

	files, h1, err := renderConfig(sp)
	if err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"shibboleth2.xml", "attribute-map.xml", "nginx.conf"} {
		if strings.TrimSpace(files[name]) == "" {
			t.Errorf("files[%q] is empty", name)
		}
	}

	if !strings.Contains(files["shibboleth2.xml"], sp.Spec.EntityID) {
		t.Error("shibboleth2.xml does not contain the spec's entityID")
	}
	if !strings.Contains(files["shibboleth2.xml"], `handlerURL="/Shibboleth.sso"`) {
		t.Error("shibboleth2.xml does not contain a relative handlerURL")
	}

	_, h2, err := renderConfig(sp)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h2 {
		t.Error("hash not stable across two identical calls")
	}
	if h1 == "" {
		t.Error("hash is empty")
	}

	// A no-op re-derivation from an identical spec (fresh struct, same
	// values) must hash the same.
	spSame := newSampleSPInstance()
	_, h3, err := renderConfig(spSame)
	if err != nil {
		t.Fatal(err)
	}
	if h1 != h3 {
		t.Error("hash not stable for an equivalent but distinct SPInstance value")
	}

	// A spec change that alters rendered content must change the hash.
	spChanged := newSampleSPInstance()
	spChanged.Spec.IdP.MetadataURL = "https://idp.example.org/metadata-changed"
	_, h4, err := renderConfig(spChanged)
	if err != nil {
		t.Fatal(err)
	}
	if h4 == h1 {
		t.Error("hash did not change after an unrelated metadata-field change")
	}
}
