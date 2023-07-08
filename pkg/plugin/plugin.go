/*
This code is a POC.  It is NOT stable and not for production use!

See README.md for maturity gaps.
*/
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	"github.com/bensolo-io/rollouts-plugin-trafficrouter-glooplatform/pkg/util"
	"github.com/sirupsen/logrus"
	solov2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Type                       = "GlooPlatformAPI"
	GlooPlatformAPIUpdateError = "GlooPlatformAPIUpdateError"
	PluginName                 = "solo-io/glooplatformAPI"
)

type RpcPlugin struct {
	IsTest bool
	// temporary hack until mock clienset is fixed (missing some interface methods)
	TestRouteTable *networkv2.RouteTable
	LogCtx         *logrus.Entry
	Client         networkv2.Clientset
}

type GlooPlatformAPITrafficRouting struct {
	RouteTableName       string `json:"routeTableName" protobuf:"bytes,1,name=routeTableName"`
	RouteTableNamespace  string `json:"routeTableNamespace" protobuf:"bytes,2,name=routeTableNamespace"`
	DestinationKind      string `json:"destinationKind" protobuf:"bytes,2,name=destinationKind"`
	DestinationNamespace string `json:"destinationNamespace" protobuf:"bytes,2,name=destinationNamespace"`
}

type GlooMatchedRouteTable struct {
	// matched gloo platform route table
	RouteTable *networkv2.RouteTable
	// matched http routes within the routetable
	HttpRoutes []*GlooMatchedHttpRoutes
	// matched tcp routes within the routetable
	TCPRoutes []*GlooMatchedTCPRoutes
	// matched tls routes within the routetable
	TLSRoutes []*GlooMatchedTLSRoutes
}

type GlooMatchedHttpRoutes struct {
	// matched HttpRoute
	HttpRoute *networkv2.HTTPRoute
	// matched destinations within the httpRoute
	Destinations []*solov2.DestinationReference
}

type GlooMatchedTLSRoutes struct {
	// matched HttpRoute
	TLSRoute *networkv2.TLSRoute
	// matched destinations within the httpRoute
	Destinations []*solov2.DestinationReference
}

type GlooMatchedTCPRoutes struct {
	// matched HttpRoute
	TCPRoute *networkv2.TCPRoute
	// matched destinations within the httpRoute
	Destinations []*solov2.DestinationReference
}

func (g *GlooPlatformAPITrafficRouting) matchesObjectRef(ref *solov2.ObjectReference, serviceName string) bool {
	return ref != nil &&
		strings.EqualFold(ref.Namespace, g.DestinationNamespace) &&
		strings.EqualFold(ref.Name, serviceName)
}

func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}

	r.LogCtx = r.LogCtx.WithField("PluginName", PluginName)
	k, err := util.NewSoloNetworkV2K8sClient()
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}
	r.Client = k
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

	// TODO get matching RTs, routes, and destinations
	var rts []*GlooMatchedRouteTable

	if rollout.Spec.Strategy.Canary != nil {
		return r.handleCanary(rollout, rts)
	} else if rollout.Spec.Strategy.BlueGreen != nil {
		return r.handleBlueGreen(rollout)
	}

	var rt *networkv2.RouteTable

	if !r.IsTest {
		rt, err = r.getRouteTable(ctx, glooplatformConfig)
		if err != nil {
			return pluginTypes.RpcError{
				ErrorString: err.Error(),
			}
		}
	} else if r.TestRouteTable != nil {
		rt = r.TestRouteTable
	}

	// do we need this (not sure if not found yields an error)?
	if rt == nil {
		return pluginTypes.RpcError{
			ErrorString: fmt.Sprintf("rt not found: %s.%s", glooplatformConfig.RouteTableNamespace, glooplatformConfig.RouteTableName),
		}
	}

	r.LogCtx.Debugf("found RT %s", rt.Name)

	// get the stable destination
	httpRoute, stableDest, canaryDest, err := getHttpRefs(rollout.Spec.Strategy.Canary.StableService, rollout.Spec.Strategy.Canary.CanaryService, glooplatformConfig, rt)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	if stableDest == nil {
		return pluginTypes.RpcError{
			ErrorString: fmt.Sprintf("failed to find RT %s.%s", glooplatformConfig.RouteTableNamespace, glooplatformConfig.RouteTableName),
		}
	}
	remainingWeight := 100 - desiredWeight
	stableDest.Weight = uint32(remainingWeight)

	// if this is first step, the canary route may need to be created
	if canaryDest == nil {
		// {"RefKind":{"Ref":{"name":"httpbin","namespace":"httpbin"}},"port":{"Specifier":{"Number":8000}},"weight":100}
		canaryDest = &solov2.DestinationReference{
			Kind: stableDest.Kind,
			Port: &solov2.PortSelector{
				Specifier: &solov2.PortSelector_Number{
					Number: stableDest.Port.GetNumber(),
				},
			},
			RefKind: &solov2.DestinationReference_Ref{
				Ref: &solov2.ObjectReference{
					Name:      rollout.Spec.Strategy.Canary.CanaryService,
					Namespace: stableDest.GetRef().Namespace,
				},
			},
		}
		httpRoute.GetForwardTo().Destinations = append(httpRoute.GetForwardTo().Destinations, canaryDest)
	}

	canaryDest.Weight = uint32(desiredWeight)

	r.LogCtx.Debugf("attempting to set stable=%d, canary=%d", stableDest.Weight, canaryDest.Weight)

	if !r.IsTest {
		err = r.Client.RouteTables().UpdateRouteTable(ctx, rt, &k8sclient.UpdateOptions{})
		if err != nil {
			r.LogCtx.Error(err.Error())
			return pluginTypes.RpcError{
				ErrorString: err.Error(),
			}
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
	// we could remove the canary destination, but not required since it will have 0 weight at the end of rollout
	return pluginTypes.RpcError{}
}

func (r *RpcPlugin) Type() string {
	return Type
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*GlooPlatformAPITrafficRouting, error) {
	glooplatformConfig := GlooPlatformAPITrafficRouting{}

	err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[PluginName], &glooplatformConfig)
	if err != nil {
		return nil, err
	}

	return &glooplatformConfig, nil
}

func (r *RpcPlugin) handleFoo() {

}

func (r *RpcPlugin) getRouteTable(ctx context.Context, trafficConfig *GlooPlatformAPITrafficRouting) (*networkv2.RouteTable, error) {
	return r.Client.RouteTables().GetRouteTable(ctx, k8sclient.ObjectKey{
		Namespace: trafficConfig.RouteTableNamespace,
		Name:      trafficConfig.RouteTableName,
	})
}

func getHttpRefs(stableServiceName string, canaryServiceName string, trafficConfig *GlooPlatformAPITrafficRouting, rt *networkv2.RouteTable) (route *networkv2.HTTPRoute, stable *solov2.DestinationReference, canary *solov2.DestinationReference, err error) {
	for _, httpRoute := range rt.Spec.Http {
		fw := httpRoute.GetForwardTo()
		if fw != nil {
			for _, dest := range fw.Destinations {
				if strings.EqualFold(dest.Kind.String(), trafficConfig.DestinationKind) {
					ref := dest.GetRef()
					if trafficConfig.matchesObjectRef(ref, stableServiceName) {
						route = httpRoute
						stable = dest
						continue
					}
					if trafficConfig.matchesObjectRef(ref, canaryServiceName) {
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

	if route == nil {
		err = fmt.Errorf("failed to find an http route that references stable service %s in RouteTable %s.%s", stableServiceName, rt.Namespace, rt.Name)
		return
	}

	err = fmt.Errorf("failed to find a destination that references stable service %s in RouteTable %s.%s in http route %s", stableServiceName, rt.Namespace, rt.Name, route.Name)
	return
}
