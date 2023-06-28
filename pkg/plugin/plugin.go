/* POC to support argo rollouts for Gloo Platform.  This is a POC to demonstrate the argo architecture.
 * TODO:
 * - support different forwardTo.destination.kinds
 * - account for a matched destination already having "static" (non-canary) weighted routing
 * - handle different destination types between stable and canary
 * - remove canary destination when rollout is complete (right now just sets to 0 weight)
 * - clean up duplicated code in getHttpRefs
 */
package plugin

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/sirupsen/logrus"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	clientcmd "k8s.io/client-go/tools/clientcmd"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Type                       = "GlooPlatformAPI"
	GlooPlatformAPIUpdateError = "GlooPlatformAPIUpdateError"
	PluginName                 = "solo-io/glooplatformAPI"
)

type RpcPlugin struct {
	IsTest bool
	LogCtx *logrus.Entry
	Client networkv2.Clientset
}

type GlooPlatformAPITrafficRouting struct {
	RouteTableName       string `json:"routeTableName" protobuf:"bytes,1,name=routeTableName"`
	RouteTableNamespace  string `json:"routeTableNamespace" protobuf:"bytes,2,name=routeTableNamespace"`
	DestinationKind      string `json:"destinationKind" protobuf:"bytes,2,name=destinationKind"`
	DestinationNamespace string `json:"destinationNamespace" protobuf:"bytes,2,name=destinationNamespace"`
}

func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}
	cfg, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	kubeConfig := clientcmd.NewDefaultClientConfig(*cfg, &clientcmd.ConfigOverrides{})
	clientConfig, err := kubeConfig.ClientConfig()
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	networkv2Clientset, err := networkv2.NewClientsetFromConfig(clientConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	r.Client = networkv2Clientset
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) UpdateHash(rollout *v1alpha1.Rollout, canaryHash, stableHash string, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	glooplatformConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	rt, err := r.getRouteTable(ctx, glooplatformConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	// do we need this?
	if rt == nil {
		logrus.Debugf("rt not found: %s.%s", glooplatformConfig.RouteTableNamespace, glooplatformConfig.RouteTableName)
		return pluginTypes.RpcError{}
	}

	// get the stable destination
	_, stableDest, canaryDest := getHttpRefs(rollout.Spec.Strategy.Canary.StableService, rollout.Spec.Strategy.Canary.CanaryService, glooplatformConfig, rt)

	remainingWeight := 100 - desiredWeight
	stableDest.Weight = uint32(remainingWeight)

	// if this is first step, the canary route must be created
	// this a dumb clone of the stable destination for POC purposes
	if canaryDest == nil {
		b, err := json.Marshal(stableDest)
		if err != nil {
			return pluginTypes.RpcError{
				ErrorString: err.Error(),
			}
		}
		canaryDest = &solov2.DestinationReference{}
		err = json.Unmarshal(b, canaryDest)
		if err != nil {
			return pluginTypes.RpcError{
				ErrorString: err.Error(),
			}
		}
		canaryDest.GetRef().Name = rollout.Spec.Strategy.Canary.CanaryService
	}
	canaryDest.Weight = uint32(desiredWeight)

	err = r.Client.RouteTables().UpdateRouteTable(ctx, rt, &k8sclient.UpdateOptions{})
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetHeaderRoute(rollout *v1alpha1.Rollout, headerRouting *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) SetMirrorRoute(rollout *v1alpha1.Rollout, setMirrorRoute *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) VerifyWeight(rollout *v1alpha1.Rollout, desiredWeight int32, additionalDestinations []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.Verified, pluginTypes.RpcError{}
}

func (r *RpcPlugin) RemoveManagedRoutes(rollout *v1alpha1.Rollout) pluginTypes.RpcError {
	ctx := context.TODO()
	glooplatformConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	rt, err := r.getRouteTable(ctx, glooplatformConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	_, stableDest, canaryDest := getHttpRefs(rollout.Spec.Strategy.Canary.StableService, rollout.Spec.Strategy.Canary.CanaryService, glooplatformConfig, rt)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	// TODO - actually remove the destination instead of setting 0 weight
	if canaryDest != nil {
		canaryDest.Weight = 0
	}
	stableDest.Weight = 100

	err = r.Client.RouteTables().UpdateRouteTable(ctx, rt, &k8sclient.UpdateOptions{})
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) Type() string {
	return Type
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*GlooPlatformAPITrafficRouting, error) {
	glooplatformConfig := GlooPlatformAPITrafficRouting{}

	// not sure if this is required - do all plugins get all rollouts routed to them?
	if _, pluginFound := rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName]; !pluginFound {
		return nil, nil
	}

	err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName], &glooplatformConfig)
	if err != nil {
		return nil, err
	}

	return &glooplatformConfig, nil
}

func (r *RpcPlugin) getRouteTable(ctx context.Context, trafficConfig *GlooPlatformAPITrafficRouting) (*networkv2.RouteTable, error) {
	return r.Client.RouteTables().GetRouteTable(ctx, k8sclient.ObjectKey{
		Namespace: trafficConfig.RouteTableNamespace,
		Name:      trafficConfig.RouteTableName,
	})
}

func getHttpRefs(stableServiceName string, canaryServiceName string, trafficConfig *GlooPlatformAPITrafficRouting, rt *networkv2.RouteTable) (route *networkv2.HTTPRoute, stable *solov2.DestinationReference, canary *solov2.DestinationReference) {
	for _, httpRoute := range rt.Spec.Http {
		fw := httpRoute.GetForwardTo()
		if fw != nil {
			for _, dest := range fw.Destinations {
				if strings.EqualFold(dest.Kind.String(), trafficConfig.DestinationKind) {
					ref := dest.GetRef()
					// did we find the stable ref?
					if ref != nil &&
						strings.EqualFold(ref.Namespace, trafficConfig.DestinationNamespace) &&
						strings.EqualFold(ref.Name, stableServiceName) {
						route = httpRoute
						stable = dest
						continue
					}
					// TODO clean up duplicate code (only difference is stable vs. canary)
					if ref != nil &&
						strings.EqualFold(ref.Namespace, trafficConfig.DestinationNamespace) &&
						strings.EqualFold(ref.Name, canaryServiceName) {
						canary = dest
						continue
					}
				}
			}
		}
		// if the stable ref is found, return whether or not the dest was found;
		// if the dest doesn't exist yet it will get created
		if stable != nil {
			return
		}
	} // end http route loop
	return nil, nil, nil
}
