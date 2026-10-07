# Build and release

A push to `main` on Forgejo builds and publishes `ronaldraygun/telegram-relay`
with no manual step:

```
git push -> Forgejo webhook (stamped by forgejo-init) -> webhooks-ci.ardenone.com/telegram-relay
  -> Argo Events `telegram-relay-sensor` -> WorkflowTemplate `telegram-relay-build` (iad-ci)
```

All of it is declared in `jedarden/declarative-config` (`k8s/iad-ci/argo-events/` and
`k8s/iad-ci/argo-workflows/telegram-relay-build-workflowtemplate.yml`).

- If the pushed commit changed `VERSION`, that value is the image tag; otherwise the patch
  number is incremented from `VERSION` (the commit message of the auto-bump says which).
- The build publishes the image only. The running version is pinned in
  `k8s/ardenone-cluster/telegram-relay/deployment.yml`; change that pin in
  declarative-config once the tag exists (`docker manifest inspect
  ronaldraygun/telegram-relay:<tag>`).
- `POST /alertmanager` (0.2.0) is what the iad-kalshi and apexalgo-iad Alertmanagers call.
