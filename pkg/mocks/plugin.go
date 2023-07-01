package mocks

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonv2 "github.com/solo-io/solo-apis/client-go/common.gloo.solo.io/v2"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
)

const (
	RouteTableName       = "httpbin"
	RouteTableNamespace  = "httpbin"
	DestinationKind      = "SERVICE"
	DestinationNamespace = "httpbin"
)

var RouteTable = networkv2.RouteTable{
	ObjectMeta: metav1.ObjectMeta{
		Name:      RouteTableName,
		Namespace: RouteTableNamespace,
	},
	Spec: networkv2.RouteTableSpec{
		Hosts: []string{"*"},

		Http: []*networkv2.HTTPRoute{
			{
				Name: RouteTableName,
				ActionType: &networkv2.HTTPRoute_ForwardTo{
					ForwardTo: &networkv2.ForwardToAction{
						Destinations: []*commonv2.DestinationReference{
							{
								Kind: commonv2.DestinationKind_SERVICE,
								Port: &commonv2.PortSelector{
									Specifier: &commonv2.PortSelector_Number{
										Number: 8000,
									},
								},
								RefKind: &commonv2.DestinationReference_Ref{
									Ref: &commonv2.ObjectReference{
										Name:      RouteTableName,
										Namespace: DestinationNamespace,
									},
								},
							},
						},
					},
				},
			},
		},
	},
}
