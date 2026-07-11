# saml-sp-operator

A Kubernetes operator that wraps a containerized **Shibboleth SP v3** as a
gateway-portable, forward-auth authenticator. It owns the orchestration and
borrows the SAML: the operator does zero cryptography. It watches two CRDs plus
Gateway API `HTTPRoute`s, renders `shibboleth2.xml` + `nginx.conf` into a
ConfigMap, rolls the SP Deployment on config change, and emits the gateway
attachment (a Traefik ForwardAuth `Middleware` today; GEP-1494 `ExternalAuth` as
it matures). See [`DESIGN.md`](DESIGN.md) for the full decision record.

## The CRDs

| Kind | Namespace | Holds |
|------|-----------|-------|
| `SPInstance` | central auth ns | entityID, keypair Secret, IdP/federation trust, an `allowedNamespaces` consent selector, session-store reference |
| `AppIntegration` | app ns, beside the `HTTPRoute` | `targetRef` to the route, the `SPInstance` to bind, attribute→header mapping, per-app session/authz policy |

Many `AppIntegration` → one `SPInstance` is a shared multi-tenant authenticator
(one entityID, cross-app SSO); 1:1 is dedicated. The schema never chooses; the
operator reconciles whatever graph it is handed.

Group: `saml.tickletechnologies.com`. Version: `v1alpha1` (in flux — the API is not yet
stable).

## Status

Early. This repository currently contains:

- **The API scaffold** — kubebuilder project, the two CRD types, generated CRDs
  and RBAC. Controller reconcile logic is not implemented yet.
- **[`spike/`](spike/)** — the manual de-risking spike that proved the whole
  config surface the operator must generate end to end against a real IdP:
  `SHIBSP_SERVER_*` env + absolute `handlerURL`, the RequestMap `<Host scheme
  port>` rule, a headless ForwardAuth target, attribute-map + `Variable-<id>`
  response headers, and the edge header-hygiene contract. Its runbook is
  [`spike/README.md`](spike/README.md).

## Developing

```sh
make manifests   # regenerate CRDs + RBAC from the +kubebuilder markers
make generate    # regenerate deepcopy code
make build       # go build the manager
make test        # unit + envtest
make run         # run the controllers against your current kubecontext
```

## License

Apache-2.0. See [`LICENSE`](LICENSE).
