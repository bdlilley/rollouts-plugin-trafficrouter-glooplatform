### Gloo Platform Plugin for Argo Rollouts

**This plugin is POC-quality and Argo Rollouts was recently released as alpha in 1.5.  You have been warned!**

This repo contains a POC implementation of an [Argo Rollouts plugin](https://argoproj.github.io/argo-Rollouts/features/traffic-management/plugins/) to support the Gloo Platform API.

Patterns were based on the K8s gateway api plugin found [here](https://github.com/argoproj-labs/rollouts-plugin-trafficrouter-gatewayapi).

### Rollouts Concepts

Think of Argo Rollout CR like a replacement for the K8s Deployment CR.  The Rollout contains 2 primary configs:

* a pod template similar to what you would use in a Deployment
* the configs and strategy for the Rollout operation (steps, weights, pauses, etc.)

While you can *reference* a Deployment as a way to get the pod template details, Rollouts still will not manage the referenced deployment directly. 

Here is the workflow:

* Create stable and canary (or blue / green) k8s services
* Create gateway/ingress resources that route to the stable service
* Do NOT create a deployment :) 
* Create a Rollout that contains a pod template that would have been used in a deployment
*  

### Example
kubectl apply -f ./examples/demo-api-initial-state
kubectl label ns gloo-Rollout-demo istio.io/rev=1-17-2 

watch -n 0.5 'curl localhost:8888/demo'

kubectl apply -f ./examples/0-Rollout-initial
curl localhost:8888/demo -s  | jq -c
kubectl apply -f ./examples/1-Rollout-first-change
curl localhost:8888/demo -s  | jq -c

### Gloo UI UX

* UI displays pod name b/c there is no deployment object with Rollouts; only Rollout CR -> replicasets;  please upvote this issue https://github.com/argoproj/argo-Rollouts/issues/2779 for supporting generation of deployment CRs
  * There may be other issues in Gloo due to missing deployment metadata; will require eng research

### TODO

- support different forwardTo.destination.kinds
- account for a matched destination already having "static" (non-canary) weighted routing
- implement label selectors for RouteTable and Route lookup
- handle different destination types between stable and canary
- remove canary destination when Rollout is complete (right now just sets to 0 weight)
- clean up duplicated code in getHttpRefs
- handle named ports
- add more advanced features to the rollout metadata that is passed to our plugin; i.e. this section could be enhanced with other Gloo capabilities:

```yaml
      trafficRouting:
        plugins:
          solo-io/glooplatformAPI:
            routeTableName: default
            routeTableNamespace: gloo-mesh
            destinationKind: SERVICE
            destinationNamespace: gloo-rollout-demo
```