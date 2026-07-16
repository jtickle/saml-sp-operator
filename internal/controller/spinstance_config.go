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
	samlv1alpha1 "github.com/jtickle/saml-sp-operator/api/v1alpha1"
	"github.com/jtickle/saml-sp-operator/internal/render"
)

// spCredentialKeyPath and spCredentialCertPath are the fixed in-pod mount
// paths for the SP signing/decryption keypair (the container contract
// shared with internal/render's shibdload_test.go). The Secret named by
// SPInstanceSpec.Credentials is projected to these paths by the Deployment
// this controller renders — never read directly by this package.
const (
	spCredentialKeyPath  = "/run/shibboleth/sp-credentials/tls.key"
	spCredentialCertPath = "/run/shibboleth/sp-credentials/tls.crt"
)

// spSessionLifetimeSeconds, spSessionTimeoutSeconds, and the remaining
// spSession* constants are 1a's in-memory session defaults (spike values —
// see the task brief). A shared memcached-backed SessionStore
// (SPInstanceSpec.SessionStore) is a later slice.
const (
	spSessionLifetimeSeconds = 28800
	spSessionTimeoutSeconds  = 3600
	spSessionRelayState      = "ss:mem"
	spSessionCheckAddress    = false
	spSessionHandlerSSL      = true
	spSessionCookieProps     = "https"
)

// fileShibboleth2, fileAttributeMap, and fileNginxConf are the file names
// of the three rendered Shibboleth SP config files. They are the keys in the
// ConfigMap's data and the SubPaths the Deployment projects them to, so both
// this package and spinstance_objects.go reference them.
const (
	fileShibboleth2  = "shibboleth2.xml"
	fileAttributeMap = "attribute-map.xml"
	fileNginxConf    = "nginx.conf"
)

// spDefaultRemoteUser is the 1a default REMOTE_USER attribute-id search
// order: no AppIntegrations exist yet in slice 1a, so this list cannot be
// derived from bound apps' attribute mappings.
var spDefaultRemoteUser = []string{"email", "uid"}

// renderConfig maps sp into a render.SPConfig and renders the three
// Shibboleth SP config files plus their combined hash. It is a pure
// function: no Kubernetes client, no cluster access, no I/O beyond the
// render package's in-memory template execution. In slice 1a no
// AppIntegrations exist, so the RequestMap winners and attribute-map
// entries are both empty — render.RenderShibboleth2 and
// render.RenderAttributeMap accept nil for both, producing a valid bare SP.
func renderConfig(sp *samlv1alpha1.SPInstance) (map[string]string, string, error) {
	cfg := render.SPConfig{
		EntityID: sp.Spec.EntityID,
		IdP: render.IdPConfig{
			MetadataURL: sp.Spec.IdP.MetadataURL,
			EntityID:    sp.Spec.IdP.EntityID,
		},
		CredentialKeyPath:  spCredentialKeyPath,
		CredentialCertPath: spCredentialCertPath,
		RemoteUser:         spDefaultRemoteUser,
		Sessions: render.SessionDefaults{
			LifetimeSeconds: spSessionLifetimeSeconds,
			TimeoutSeconds:  spSessionTimeoutSeconds,
			RelayState:      spSessionRelayState,
			CheckAddress:    spSessionCheckAddress,
			HandlerSSL:      spSessionHandlerSSL,
			CookieProps:     spSessionCookieProps,
		},
	}

	shibboleth2, err := render.RenderShibboleth2(cfg, nil)
	if err != nil {
		return nil, "", err
	}

	attributeMap, err := render.RenderAttributeMap(nil)
	if err != nil {
		return nil, "", err
	}

	nginxConf, err := render.RenderNginxConf(cfg)
	if err != nil {
		return nil, "", err
	}

	files := []render.ConfigFile{
		{Name: fileShibboleth2, Bytes: shibboleth2},
		{Name: fileAttributeMap, Bytes: attributeMap},
		{Name: fileNginxConf, Bytes: nginxConf},
	}
	hash := render.Hash(files)

	out := make(map[string]string, len(files))
	for _, f := range files {
		out[f.Name] = string(f.Bytes)
	}

	return out, hash, nil
}
