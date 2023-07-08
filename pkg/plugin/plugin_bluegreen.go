package plugin

import (
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
)

func (r *RpcPlugin) handleBlueGreen(rollout *v1alpha1.Rollout) pluginTypes.RpcError {

	return pluginTypes.RpcError{
		ErrorString: "BlueGreen not implemented",
	}
}
