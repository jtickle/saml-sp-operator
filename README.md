# saml-sp-operator

A self-hosted, Kubernetes-native SAML Service Provider for protecting apps
behind [Gateway API](https://gateway-api.sigs.k8s.io/) — built as an operator
over a containerized [Shibboleth SP](https://shibboleth.atlassian.net/wiki/spaces/SP3/overview).

The durable asset is the **orchestration** — the CRDs, the operator, the
Gateway API attachment, the session model — with the SAML engine underneath
treated as a swappable backend. We own the orchestration; we borrow the SAML.

## Status

🚧 **Early development.** This is a placeholder. Active work — design notes, the
forward-auth de-risking spike, and the build pipeline — lives on the
[`spike`](https://github.com/jtickle/saml-sp-operator/tree/spike) branch until
the approach is settled. `main` will fill in once it is.

## License

[Apache License 2.0](LICENSE) — Copyright Tickle Technologies, LLC.
