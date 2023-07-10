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
	"k8s.io/apimachinery/pkg/labels"
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
	RouteTableSelector *DumbObjectSelector `json:"routeTableSelector" protobuf:"bytes,1,name=routeTableSelector"`
	RouteSelector      *DumbRouteSelector  `json:"routeSelector" protobuf:"bytes,2,name=routeSelector"`
	// DestinationMatcher *GlooDestinationMatcher `json:"destinationMatcher" protobuf:"bytes,3,name=destinationMatcher"`
	// RouteTableName       string `json:"routeTableName" protobuf:"bytes,1,name=routeTableName"`
	// RouteTableNamespace  string `json:"routeTableNamespace" protobuf:"bytes,2,name=routeTableNamespace"`
	// DestinationKind      string `json:"destinationKind" protobuf:"bytes,2,name=destinationKind"`
	// DestinationNamespace string `json:"destinationNamespace" protobuf:"bytes,2,name=destinationNamespace"`
}

type DumbObjectSelector struct {
	Labels    map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name      string            `json:"name" protobuf:"bytes,2,name=name"`
	Namespace string            `json:"namespace" protobuf:"bytes,3,name=namespace"`
}

type DumbRouteSelector struct {
	Labels map[string]string `json:"labels" protobuf:"bytes,1,name=labels"`
	Name   string            `json:"name" protobuf:"bytes,2,name=name"`
}

type GlooDestinationMatcher struct {
	// Regexp *GlooDestinationMatcherRegexp `json:"regexp" protobuf:"bytes,1,name=regexp"`
	Ref *solov2.ObjectReference `json:"ref" protobuf:"bytes,2,name=ref"`
}

// type GlooDestinationMatcherRegexp struct {
// 	NameRegexp      *regexp.Regexp `json:"nameRegexp" protobuf:"bytes,1,name=nameRegexp"`
// 	NamespaceRegexp *regexp.Regexp `json:"namespaceRegexp" protobuf:"bytes,2,name=namespaceRegexp"`
// 	KindRegexp      *regexp.Regexp `json:"kindRegexp" protobuf:"bytes,3,name=kindRegexp"`
// 	ClusterRegexp   *regexp.Regexp `json:"clusterRegexp" protobuf:"bytes,4,name=clusterRegexp"`
// }

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

type GlooDestinations struct {
	StableOrActiveDestination  *solov2.DestinationReference
	CanaryOrPreviewDestination *solov2.DestinationReference
}

type GlooMatchedHttpRoutes struct {
	// matched HttpRoute
	HttpRoute *networkv2.HTTPRoute
	// matched destinations within the httpRoute
	Destinations *GlooDestinations
}

type GlooMatchedTLSRoutes struct {
	// matched HttpRoute
	TLSRoute *networkv2.TLSRoute
	// matched destinations within the httpRoute
	Destinations []*GlooDestinations
}

type GlooMatchedTCPRoutes struct {
	// matched HttpRoute
	TCPRoute *networkv2.TCPRoute
	// matched destinations within the httpRoute
	Destinations []*GlooDestinations
}

// func (g *GlooPlatformAPITrafficRouting) matchesObjectRef(ref *solov2.ObjectReference, serviceName string) bool {
// 	return ref != nil &&
// 		strings.EqualFold(ref.Namespace, g.DestinationNamespace) &&
// 		strings.EqualFold(ref.Name, serviceName)
// }

func (g *GlooMatchedRouteTable) matchRoutes(logCtx *logrus.Entry, rollout *v1alpha1.Rollout, trafficConfig *GlooPlatformAPITrafficRouting) error {
	if g.RouteTable == nil {
		return fmt.Errorf("matchRoutes called for nil RouteTable")
	}

	// HTTP Routes
	for _, httpRoute := range g.RouteTable.Spec.Http {
		// find the destination that matches the stable svc
		fw := httpRoute.GetForwardTo()
		if fw == nil {
			logCtx.Debugf("skipping route %s.%s becuase forwardTo is nil", g.RouteTable.Name, httpRoute.Name)
			continue
		}

		// skip non-matching routes if RouteSelector provided
		if trafficConfig.RouteSelector != nil {
			// if name was provided, skip if route name doesn't match
			if !strings.EqualFold(trafficConfig.RouteSelector.Name, "") && !strings.EqualFold(trafficConfig.RouteSelector.Name, httpRoute.Name) {
				logCtx.Debugf("skipping route %s.%s because it doesn't match route name selector %s", g.RouteTable.Name, httpRoute.Name, trafficConfig.RouteSelector.Name)
				continue
			}
			// if labels provided, skip if route labels do not contain all specified labels
			if trafficConfig.RouteSelector.Labels != nil {
				for k, v := range trafficConfig.RouteSelector.Labels {
					if vv, ok := httpRoute.Labels[k]; ok {
						if !strings.EqualFold(v, vv) {
							logCtx.Debugf("skipping route %s.%s because route labels do not contain %s=%s", g.RouteTable.Name, httpRoute.Name, k, vv)
							continue
						}
					}
				}
			}
			logCtx.Debugf("route %s.%s passed RouteSelector", g.RouteTable.Name, httpRoute.Name)
		}

		// find destinations
		// var matchedDestinations []*GlooDestinations
		var canary, stable *solov2.DestinationReference
		for _, dest := range fw.Destinations {
			ref := dest.GetRef()
			if ref == nil {
				logCtx.Debugf("skipping destination %s.%s because destination ref was nil; %+v", g.RouteTable.Name, httpRoute.Name, dest)
				continue
			}
			if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.StableService) {
				logCtx.Debugf("matched stable ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
				stable = dest
				continue
			}
			if strings.EqualFold(ref.Name, rollout.Spec.Strategy.Canary.CanaryService) {
				logCtx.Debugf("matched canary ref %s.%s.%s", g.RouteTable.Name, httpRoute.Name, ref.Name)
				canary = dest
				// bail if we found both stable and canary
				if stable != nil {
					break
				}
				continue
			}
		}

		if stable != nil {
			dest := &GlooMatchedHttpRoutes{
				HttpRoute: httpRoute,
				Destinations: &GlooDestinations{
					StableOrActiveDestination:  stable,
					CanaryOrPreviewDestination: canary,
				},
			}
			logCtx.Debugf("adding destination %+v", dest)
			g.HttpRoutes = append(g.HttpRoutes, dest)
		}
	} // end range httpRoutes

	return nil
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
	glooPluginConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	// get the matched routetables
	matchedRts, err := r.getRouteTables(ctx, glooPluginConfig)
	if err != nil {
		return pluginTypes.RpcError{
			ErrorString: err.Error(),
		}
	}

	return pluginTypes.RpcError{
		ErrorString: fmt.Sprintf("%d matched rts; plugin not implemented", len(matchedRts)),
	}

	// TODO get the matched routes and destinations from each selected routeTable

	if rollout.Spec.Strategy.Canary != nil {
		return r.handleCanary(ctx, rollout, desiredWeight, additionalDestinations, glooPluginConfig, matchedRts)
	} else if rollout.Spec.Strategy.BlueGreen != nil {
		return r.handleBlueGreen(rollout)
	}

	// var rt *networkv2.RouteTable

	// if !r.IsTest {
	// 	rt, err = r.getRouteTable(ctx, glooplatformConfig)
	// 	if err != nil {
	// 		return pluginTypes.RpcError{
	// 			ErrorString: err.Error(),
	// 		}
	// 	}
	// } else if r.TestRouteTable != nil {
	// 	rt = r.TestRouteTable
	// }

	// // do we need this (not sure if not found yields an error)?
	// if rt == nil {
	// 	return pluginTypes.RpcError{
	// 		ErrorString: fmt.Sprintf("rt not found: %s.%s", glooplatformConfig.RouteTableNamespace, glooplatformConfig.RouteTableName),
	// 	}
	// }

	// r.LogCtx.Debugf("found RT %s", rt.Name)

	// // get the stable destination
	// httpRoute, stableDest, canaryDest, err := getHttpRefs(rollout.Spec.Strategy.Canary.StableService, rollout.Spec.Strategy.Canary.CanaryService, glooPluginConfig, rt)
	// if err != nil {
	// 	return pluginTypes.RpcError{
	// 		ErrorString: err.Error(),
	// 	}
	// }

	// if stableDest == nil {
	// 	return pluginTypes.RpcError{
	// 		ErrorString: fmt.Sprintf("failed to find RT %s.%s", glooplatformConfig.RouteTableNamespace, glooplatformConfig.RouteTableName),
	// 	}
	// // }

	// remainingWeight := 100 - desiredWeight
	// stableDest.Weight = uint32(remainingWeight)

	// // if this is first step, the canary route may need to be created
	// if canaryDest == nil {
	// 	// {"RefKind":{"Ref":{"name":"httpbin","namespace":"httpbin"}},"port":{"Specifier":{"Number":8000}},"weight":100}
	// 	canaryDest = &solov2.DestinationReference{
	// 		Kind: stableDest.Kind,
	// 		Port: &solov2.PortSelector{
	// 			Specifier: &solov2.PortSelector_Number{
	// 				Number: stableDest.Port.GetNumber(),
	// 			},
	// 		},
	// 		RefKind: &solov2.DestinationReference_Ref{
	// 			Ref: &solov2.ObjectReference{
	// 				Name:      rollout.Spec.Strategy.Canary.CanaryService,
	// 				Namespace: stableDest.GetRef().Namespace,
	// 			},
	// 		},
	// 	}
	// 	httpRoute.GetForwardTo().Destinations = append(httpRoute.GetForwardTo().Destinations, canaryDest)
	// }

	// canaryDest.Weight = uint32(desiredWeight)

	// r.LogCtx.Debugf("attempting to set stable=%d, canary=%d", stableDest.Weight, canaryDest.Weight)

	// if !r.IsTest {
	// 	// bad .. needs to be a patch
	// 	err = r.Client.RouteTables().UpdateRouteTable(ctx, rt, &k8sclient.UpdateOptions{})
	// 	if err != nil {
	// 		r.LogCtx.Error(err.Error())
	// 		return pluginTypes.RpcError{
	// 			ErrorString: err.Error(),
	// 		}
	// 	}
	// }

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

func (r *RpcPlugin) getRouteTables(ctx context.Context, glooPluginConfig *GlooPlatformAPITrafficRouting) ([]*GlooMatchedRouteTable, error) {
	if !strings.EqualFold(glooPluginConfig.RouteTableSelector.Name, "") {
		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s to get single table", glooPluginConfig.RouteTableSelector.Name, glooPluginConfig.RouteTableSelector.Namespace)
		result, err := r.Client.RouteTables().GetRouteTable(ctx, k8sclient.ObjectKey{Name: glooPluginConfig.RouteTableSelector.Name, Namespace: glooPluginConfig.RouteTableSelector.Namespace})
		if err != nil {
			return nil, err
		}
		r.LogCtx.Debugf("getRouteTables using ns:name ref %s:%s found 1 table", glooPluginConfig.RouteTableSelector.Name, glooPluginConfig.RouteTableSelector.Namespace)
		return []*GlooMatchedRouteTable{
			{
				RouteTable: result,
			},
		}, nil
	}

	matched := []*GlooMatchedRouteTable{}

	opts := &k8sclient.ListOptions{}

	if glooPluginConfig.RouteTableSelector.Labels != nil {
		opts.LabelSelector = labels.SelectorFromSet(glooPluginConfig.RouteTableSelector.Labels)
	}
	if !strings.EqualFold(glooPluginConfig.RouteTableSelector.Namespace, "") {
		opts.Namespace = glooPluginConfig.RouteTableSelector.Namespace
	}

	r.LogCtx.Debugf("getRouteTables listing tables with opts %+v", opts)
	rts, err := r.Client.RouteTables().ListRouteTable(ctx, opts)
	if err != nil {
		return nil, err
	}

	r.LogCtx.Debugf("getRouteTables listing tables with opts %+v; found %d routeTables", opts, len(rts.Items))
	for _, rt := range rts.Items {
		matchedRt := &GlooMatchedRouteTable{
			RouteTable: &rt,
		}
		// destination matching
		if err := matchedRt.matchRoutes(r.LogCtx, rollout, glooPluginConfig); err != nil {
			return nil, err
		}

		matched = append(matched, matchedRt)
	}

	return matched, nil
}

// func getMatchedDestinations(stableServiceName string, canaryServiceName string, trafficConfig *GlooPlatformAPITrafficRouting, rt *networkv2.RouteTable) (route *networkv2.HTTPRoute, stable *solov2.DestinationReference, canary *solov2.DestinationReference, err error) {
// 	var httpRoutes []*networkv2.HTTPRoute

// 	if trafficConfig.RouteSelector != nil {

// 	}

// 	for _, httpRoute := range rt.Spec.Http {
// 		fw := httpRoute.GetForwardTo()

// 		}
// 		// if the stable ref is found, return whether or not the dest was found;
// 		// if the dest doesn't exist yet it will get created
// 		if stable != nil {
// 			return
// 		}
// 	} // end http route loop

// 			// if fw != nil {
// 		// 	for _, dest := range fw.Destinations {
// 		// 		if strings.EqualFold(dest.Kind.String(), trafficConfig.DestinationKind) {
// 		// 			ref := dest.GetRef()
// 		// 			if trafficConfig.matchesObjectRef(ref, stableServiceName) {
// 		// 				route = httpRoute
// 		// 				stable = dest
// 		// 				continue
// 		// 			}
// 		// 			if trafficConfig.matchesObjectRef(ref, canaryServiceName) {
// 		// 				canary = dest
// 		// 				continue
// 		// 			}
// 		// 		}
// 		// 	}

// 	// if route == nil {
// 	// 	err = fmt.Errorf("failed to find an http route that references stable service %s in RouteTable %s.%s", stableServiceName, rt.Namespace, rt.Name)
// 	// 	return
// 	// // }

// 	// err = fmt.Errorf("failed to find a destination that references stable service %s in RouteTable %s.%s in http route %s", stableServiceName, rt.Namespace, rt.Name, route.Name)
// 	// return
// }
