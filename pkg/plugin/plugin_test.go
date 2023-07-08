package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PaesslerAG/gval"
	"github.com/PaesslerAG/jsonpath"
	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	"github.com/ghodss/yaml"
	networkv2 "github.com/solo-io/solo-apis/client-go/networking.gloo.solo.io/v2"
	"github.com/stretchr/testify/assert"

	log "github.com/sirupsen/logrus"

	goPlugin "github.com/hashicorp/go-plugin"
)

var testHandshake = goPlugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "ARGO_ROLLOUTS_RPC_PLUGIN",
	MagicCookieValue: "trafficrouter",
}

type TestCase struct {
	Rollout        *v1alpha1.Rollout      `json:"rollout"`
	RouteTable     *networkv2.RouteTable  `json:"routeTable"`
	StepAssertions []StepAssertion        `json:"stepAssertions"`
	asserionMap    map[int]*StepAssertion `json:"-"`
	fileName       string                 `json:"-"`
}

type StepAssertion struct {
	Step   int                       `json:"step"`
	Assert []StepAssertionExpression `json:"assert"`
}

type StepAssertionExpression struct {
	Path string `json:"path"`
	Exp  string `json:"exp"`
}

func (tc *TestCase) Validate() error {
	var errs []string

	if len(errs) > 0 {
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (tc *TestCase) Test(t *testing.T) error {
	logCtx := log.WithFields(log.Fields{"plugin": "trafficrouter"})
	log.SetLevel(log.DebugLevel)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rpcPluginImp := &RpcPlugin{
		LogCtx:         logCtx,
		IsTest:         true,
		TestRouteTable: tc.RouteTable,
	}

	var pluginMap = map[string]goPlugin.Plugin{
		"RpcTrafficRouterPlugin": &rolloutsPlugin.RpcTrafficRouterPlugin{Impl: rpcPluginImp},
	}

	ch := make(chan *goPlugin.ReattachConfig, 1)
	closeCh := make(chan struct{})
	go goPlugin.Serve(&goPlugin.ServeConfig{
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Test: &goPlugin.ServeTestConfig{
			Context:          ctx,
			ReattachConfigCh: ch,
			CloseCh:          closeCh,
		},
	})

	var config *goPlugin.ReattachConfig
	select {
	case config = <-ch:
	case <-time.After(2000 * time.Millisecond):
		return fmt.Errorf("should've received reattach")
	}
	if config == nil {
		return fmt.Errorf("config should not be nil")
	}

	// Connect!
	c := goPlugin.NewClient(&goPlugin.ClientConfig{
		Cmd:             nil,
		HandshakeConfig: testHandshake,
		Plugins:         pluginMap,
		Reattach:        config,
	})
	client, err := c.Client()
	if err != nil {
		return fmt.Errorf("err: %s", err)
	}

	// Pinging should work
	if err := client.Ping(); err != nil {
		return fmt.Errorf("should not err: %s", err)
	}

	// Kill which should do nothing
	c.Kill()
	if err := client.Ping(); err != nil {
		return fmt.Errorf("should not err: %s", err)
	}

	// Request the plugin
	raw, err := client.Dispense("RpcTrafficRouterPlugin")
	if err != nil {
		return err
	}

	pluginInstance := raw.(*rolloutsPlugin.TrafficRouterPluginRPC)
	err = pluginInstance.InitPlugin()
	if err.Error() != "" {
		return err
	}

	t.Run(tc.fileName, func(t *testing.T) {
		if tc.Rollout.Spec.Strategy.Canary != nil {
			for index, step := range tc.Rollout.Spec.Strategy.Canary.Steps {
				if sa, ok := tc.asserionMap[index+1]; ok {
					if step.SetWeight != nil {
						rpcError := pluginInstance.SetWeight(tc.Rollout, *step.SetWeight, []v1alpha1.WeightDestination{})
						assert.Empty(t, rpcError.ErrorString)

						jsonRtBytes, err := json.Marshal(tc.RouteTable)
						assert.Empty(t, err, "failed to marshal test case RouteTable")

						// raw json is used for jsonpath expressions in test case files
						rawJsonRt := interface{}(nil)
						err = json.Unmarshal(jsonRtBytes, &rawJsonRt)
						assert.Empty(t, err, "failed to unmarshal test case RouteTable")

						for _, assertion := range sa.Assert {
							gvalParams := map[string]interface{}{}

							jPathValue, err := jsonpath.Get(assertion.Path, rawJsonRt)
							assert.Empty(t, err, "failed to resolve jsonPath expression")

							switch v := jPathValue.(type) {
							case []interface{}:
								// github.com/PaesslerAG/jsonpath is a little wonky when a filter expression is used in the path query
								// if a filter was used, the result is always []interface{}
								wasFilter := func() bool {
									if len(v) == 1 {
										switch filterV := v[0].(type) {
										case string:
											gvalParams["value"] = filterV
											gvalParams["len"] = len(filterV)
											return true
										case int:
											gvalParams["value"] = filterV
											return true
										case int8:
											gvalParams["value"] = filterV
											return true
										case int16:
											gvalParams["value"] = filterV
											return true
										case int32:
											gvalParams["value"] = filterV
											return true
										case int64:
											gvalParams["value"] = filterV
											return true
										case float32:
											gvalParams["value"] = filterV
											return true
										case float64:
											gvalParams["value"] = filterV
											return true
										case []interface{}:
											gvalParams["value"] = filterV
											gvalParams["len"] = len(filterV)
											return true
										default:
											t.Fatalf("WARNING: test case parser doesn't understand FILTERED type %T", v[0])
										}

									}
									return false
								}()

								if !wasFilter {
									gvalParams["len"] = len(v)
									gvalParams["value"] = v
								}
							case map[string]interface{}:
								gvalParams["len"] = len(v)
								gvalParams["value"] = v
							case string:
								gvalParams["len"] = len(v)
								gvalParams["value"] = v
							default:
								t.Fatalf("test case parser doesn't understand type %T", v)
							}

							gvalResult, err := gval.Evaluate(assertion.Exp, gvalParams)
							assert.Empty(t, err)
							if isTrue, ok := gvalResult.(bool); ok {
								assert.Equal(t, isTrue, true, "expression '%s' for path '%s' was false; value: %v (%T)", assertion.Exp, assertion.Path, gvalParams["value"], gvalParams["value"])
							} else {
								t.Logf("expression %s is not a bool expression", assertion.Exp)
								t.Fail()
							}
						}
						// assert.NotEqual(t, len(tc.RouteTable.Spec.Http[0].GetForwardTo().Destinations), 2)

					}
				}
			}
		}
	})

	// Canceling should cause an exit
	cancel()
	<-closeCh

	return nil
}

func TestRollouts(t *testing.T) {

	err := filepath.Walk("testfiles",
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if !info.IsDir() && filepath.Ext(path) == ".yaml" {
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("failed to read bytes from '%s': %s", path, err)
				}

				tc := &TestCase{
					fileName: info.Name(),
				}

				err = yaml.Unmarshal(data, tc)
				if err != nil {
					return fmt.Errorf("failed to unmarshal test case: %s", err)
				}

				if err := tc.Validate(); err != nil {
					return err
				}

				tc.asserionMap = make(map[int]*StepAssertion)

				for _, sa := range tc.StepAssertions {
					if existingSa, ok := tc.asserionMap[sa.Step]; ok {
						existingSa.Assert = append(existingSa.Assert, sa.Assert...)
					} else {
						tc.asserionMap[sa.Step] = &sa
					}
				}

				if err := tc.Test(t); err != nil {
					return err
				}
			}

			return nil
		})

	assert.Empty(t, err)
}
