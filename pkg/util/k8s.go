package util

import (
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func NewSoloNetworkV2K8sClient() (networkv2.Clientset, error) {
	cfg, err := GetKubeConfig()
	if err != nil {
		return nil, err
	}

	networkv2Clientset, err := networkv2.NewClientsetFromConfig(cfg)
	if err != nil {
		return nil, err
	}

	return networkv2Clientset, nil
}

func GetKubeConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	// if you want to change the loading rules (which files in which order), you can do so here
	configOverrides := &clientcmd.ConfigOverrides{}
	// if you want to change override values or bind them to flags, there are methods to help you
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, pluginTypes.RpcError{ErrorString: err.Error()}
	}
	return config, nil
}
