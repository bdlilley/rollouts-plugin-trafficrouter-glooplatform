package plugin

import (
	"context"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/bensolo-io/rollouts-plugin-trafficrouter-glooplatform/pkg/gloo"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *RpcPlugin) handleCanary(ctx context.Context, rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination, glooPluginConfig *GlooPlatformAPITrafficRouting, glooMatchedRouteTables []*GlooMatchedRouteTable) pluginTypes.RpcError {
	remainingWeight := 100 - desiredWeight

	for _, rt := range glooMatchedRouteTables {
		for _, matchedHttpRoute := range rt.HttpRoutes {
			for _, dest := range matchedHttpRoute.Destinations {
				dest.StableOrActiveDestination.Weight = uint32(remainingWeight)

				if dest.CanaryOrPreviewDestination == nil {
					dest.CanaryOrPreviewDestination = r.newCanaryDest()
					matchedHttpRoute.HttpRoute.GetForwardTo().Destinations = append(matchedHttpRoute.HttpRoute.GetForwardTo().Destinations, dest.CanaryOrPreviewDestination)
				}

				dest.CanaryOrPreviewDestination.Weight = uint32(desiredWeight)
			}
		}

		// build patches
		desiredRt := rt
		patch, modified, err := gloo.BuildRouteTablePatch(rt.RouteTable, desiredRt.RouteTable, gloo.WithAnnotations(), gloo.WithLabels(), gloo.WithSpec())
		if err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
		if !modified {
			return pluginTypes.RpcError{}
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

func (r *RpcPlugin) newCanaryDest() *solov2.DestinationReference {
	// canaryDest = &solov2.DestinationReference{
	// 	Kind: stableDest.Kind,
	// 	Port: &solov2.PortSelector{
	// 		Specifier: &solov2.PortSelector_Number{
	// 			Number: stableDest.Port.GetNumber(),
	// 		},
	// 	},
	// 	RefKind: &solov2.DestinationReference_Ref{
	// 		Ref: &solov2.ObjectReference{
	// 			Name:      rollout.Spec.Strategy.Canary.CanaryService,
	// 			Namespace: stableDest.GetRef().Namespace,
	// 		},
	// 	},
	// }
	return &solov2.DestinationReference{}
}
