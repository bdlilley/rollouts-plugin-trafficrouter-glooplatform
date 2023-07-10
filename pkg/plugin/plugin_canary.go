package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/bensolo-io/rollouts-plugin-trafficrouter-glooplatform/pkg/gloo"
	"github.com/jinzhu/copier"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RpcPlugin) handleCanary(ctx context.Context, rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination, glooPluginConfig *GlooPlatformAPITrafficRouting, glooMatchedRouteTables []*GlooMatchedRouteTable) pluginTypes.RpcError {
	remainingWeight := 100 - desiredWeight

	for _, rt := range glooMatchedRouteTables {
		for _, matchedHttpRoute := range rt.HttpRoutes {
			if matchedHttpRoute.Destinations != nil {
				matchedHttpRoute.Destinations.StableOrActiveDestination.Weight = uint32(remainingWeight)

				if matchedHttpRoute.Destinations.CanaryOrPreviewDestination == nil {
					newDest, err := r.newCanaryDest(matchedHttpRoute.Destinations.StableOrActiveDestination, rollout)
					if err != nil {
						return pluginTypes.RpcError{
							ErrorString: err.Error(),
						}
					}
					matchedHttpRoute.Destinations.CanaryOrPreviewDestination = newDest
					matchedHttpRoute.HttpRoute.GetForwardTo().Destinations = append(matchedHttpRoute.HttpRoute.GetForwardTo().Destinations, matchedHttpRoute.Destinations.CanaryOrPreviewDestination)
				}

				matchedHttpRoute.Destinations.CanaryOrPreviewDestination.Weight = uint32(desiredWeight)
			}
		}

		// build patches
		desiredRt := rt

		patch, modified, err := gloo.BuildRouteTablePatch(rt.RouteTable, desiredRt.RouteTable, gloo.WithAnnotations(), gloo.WithLabels(), gloo.WithSpec())
		if err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
		if !modified {
			r.LogCtx.Debugf("not udpating rt %s.%s because patch would not modify it", rt.RouteTable.Namespace, rt.RouteTable.Name)
			return pluginTypes.RpcError{}
		}

		r.LogCtx.Debugf("raw patch %s", string(patch))
		return pluginTypes.RpcError{
			ErrorString: "plugin not implemented",
		}

		clientPatch := client.RawPatch(types.StrategicMergePatchType, patch)

		if !r.IsTest {
			if err := r.Client.RouteTables().PatchRouteTable(ctx, rt.RouteTable, clientPatch); err != nil {
				return pluginTypes.RpcError{
					ErrorString: fmt.Sprintf("failed to patch RouteTable: %s", err),
				}
			}
		}
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) newCanaryDest(stableDest *solov2.DestinationReference, rollout *v1alpha1.Rollout) (*solov2.DestinationReference, error) {
	canaryDest := &solov2.DestinationReference{}
	if err := copier.Copy(canaryDest, stableDest); err != nil {
		return nil, err
	}
	canaryDest.GetRef().Name = rollout.Spec.Strategy.Canary.CanaryService
	return canaryDest, nil
}
