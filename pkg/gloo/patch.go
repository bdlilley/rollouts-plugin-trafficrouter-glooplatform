package gloo

import (
	"encoding/json"

	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

type patchConfig struct {
	withAnnotations bool
	withLabels      bool
	withSpec        bool
}

type PatchOption func(p *patchConfig)

func WithAnnotations() PatchOption {
	return func(p *patchConfig) {
		p.withAnnotations = true
	}
}

func WithLabels() PatchOption {
	return func(p *patchConfig) {
		p.withLabels = true
	}
}

func WithSpec() PatchOption {
	return func(p *patchConfig) {
		p.withSpec = true
	}
}

func BuildRouteTablePatch(current, desired *networkv2.RouteTable, opts ...PatchOption) ([]byte, bool, error) {
	cfg := &patchConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	cur := &networkv2.RouteTable{}
	des := &networkv2.RouteTable{}

	if cfg.withAnnotations {
		cur.Annotations = current.Annotations
		des.Annotations = desired.Annotations
	}
	if cfg.withLabels {
		cur.Labels = current.Labels
		des.Labels = desired.Labels
	}
	if cfg.withSpec {
		cur.Spec = current.Spec
		des.Spec = desired.Spec
	}

	return createTwoWayMergePatch(cur, des, networkv2.RouteTable{})
}

func createTwoWayMergePatch(orig, new, dataStruct interface{}) ([]byte, bool, error) {
	origBytes, err := json.Marshal(orig)
	if err != nil {
		return nil, false, err
	}
	newBytes, err := json.Marshal(new)
	if err != nil {
		return nil, false, err
	}
	patch, err := strategicpatch.CreateTwoWayMergePatch(origBytes, newBytes, dataStruct)
	if err != nil {
		return nil, false, err
	}
	return patch, string(patch) != "{}", nil
}
