### Gloo Platform Plugin for Argo Rollouts

This repo contains a POC implementation of an [Argo Rollouts plugin](https://argoproj.github.io/argo-rollouts/features/traffic-management/plugins/) to support the Gloo Platform API.

Patterns were based on the K8s gateway api plugin found [here](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi).

### Rollouts Concepts

Think of Argo Rollout CR like a replacement for the K8s Deployment CR.  

### TODO

- support different forwardTo.destination.kinds
- account for a matched destination already having "static" (non-canary) weighted routing
- implement label selectors for RouteTable and Route lookup
- handle different destination types between stable and canary
- remove canary destination when rollout is complete (right now just sets to 0 weight)
- clean up duplicated code in getHttpRefs
- handle named ports