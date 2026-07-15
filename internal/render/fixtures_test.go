package render

// fixtures_test.go provides shared sample-data builders reused by every
// _test.go file in this package (types_test.go and downstream plans'
// selfurl_test.go, resolve_test.go, and later renderer tests). Keeping one
// source of sample literals means every test exercises the same shapes, and
// a shape change only needs updating here.

// SampleSPConfig returns a minimal-but-valid SPConfig literal. Its purpose is
// twofold: prove SPConfig's declared fields compile as a usable literal, and
// give downstream renderer tests (Plans 2-6) a shared starting SPConfig.
func SampleSPConfig() SPConfig {
	return SPConfig{
		EntityID: "https://sp.example.com/shibboleth",
		IdP: IdPConfig{
			MetadataURL: "https://idp.example.com/metadata",
			EntityID:    "https://idp.example.com/entityid",
		},
		CredentialKeyPath:  "/run/shibboleth/sp-credentials/tls.key",
		CredentialCertPath: "/run/shibboleth/sp-credentials/tls.crt",
		RemoteUser:         []string{"email", "uid"},
		Sessions: SessionDefaults{
			LifetimeSeconds: 28800,
			TimeoutSeconds:  3600,
			RelayState:      "ss:mem",
			CheckAddress:    false,
			HandlerSSL:      true,
			CookieProps:     "https",
		},
		ExternalURL: "https://sp.example.com:30443",
	}
}

// SampleAppBindings returns a []AppBinding fixture covering both the
// colliding and non-colliding cases every Resolve test needs:
//
//   - bindingCollideLowerUID and bindingCollideHigherUID share the same
//     (Hostname, Path), the same Priority, and the same whole-second
//     CreatedAtUnix — a deliberate same-second tie that can only resolve via
//     the UID tiebreak (D-07). bindingCollideLowerUID's UID sorts
//     lexicographically lower, so it is the expected Resolve winner.
//   - bindingHighPriority collides on the same (Hostname, Path) too, but with
//     a strictly higher Priority and an older CreatedAtUnix — it must win
//     over both same-priority bindings, proving Priority is consulted before
//     CreatedAtUnix.
//   - bindingSolo and bindingSoloOtherPath don't collide with anything (a
//     distinct hostname and a distinct path on the shared hostname,
//     respectively) and must always land in Winners with an empty Conflicts
//     contribution.
func SampleAppBindings() []AppBinding {
	return []AppBinding{
		{
			Namespace:      "team-a",
			Name:           "app-a",
			UID:            "aaaa-0001",
			Hostname:       "apps.example.com",
			Path:           "/widgets",
			Scheme:         "https",
			Port:           30443,
			Priority:       0,
			CreatedAtUnix:  1_700_000_000,
			RequireSession: true,
			Attributes: []AttributeMapping{
				{Name: "email", ExportedID: "X-Remote-User"},
			},
		},
		{
			Namespace:      "team-b",
			Name:           "app-b",
			UID:            "bbbb-0002",
			Hostname:       "apps.example.com",
			Path:           "/widgets",
			Scheme:         "https",
			Port:           30443,
			Priority:       0,
			CreatedAtUnix:  1_700_000_000,
			RequireSession: true,
		},
		{
			Namespace:      "team-c",
			Name:           "app-c",
			UID:            "cccc-0003",
			Hostname:       "apps.example.com",
			Path:           "/widgets",
			Scheme:         "https",
			Port:           30443,
			Priority:       10,
			CreatedAtUnix:  1_600_000_000,
			RequireSession: true,
		},
		{
			Namespace:      "team-d",
			Name:           "app-d",
			UID:            "dddd-0004",
			Hostname:       "other.example.com",
			Path:           "/",
			Scheme:         "https",
			Port:           30443,
			Priority:       0,
			CreatedAtUnix:  1_650_000_000,
			RequireSession: true,
		},
		{
			Namespace:      "team-e",
			Name:           "app-e",
			UID:            "eeee-0005",
			Hostname:       "apps.example.com",
			Path:           "/gizmos",
			Scheme:         "https",
			Port:           30443,
			Priority:       0,
			CreatedAtUnix:  1_650_000_500,
			RequireSession: false,
			Attributes: []AttributeMapping{
				{Name: "uid", ExportedID: "X-Remote-Uid"},
			},
		},
	}
}
