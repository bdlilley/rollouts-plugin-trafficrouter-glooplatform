package plugin

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func (r *RpcPlugin) handleCanary(rollout *v1alpha1.Rollout, glooResources []*GlooMatchedRouteTable) pluginTypes.RpcError {

	return r.InitPlugin()
}
