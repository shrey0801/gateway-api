/*
Copyright The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package tests

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	"sigs.k8s.io/gateway-api/conformance/utils/kubernetes"
	confsuite "sigs.k8s.io/gateway-api/conformance/utils/suite"
	"sigs.k8s.io/gateway-api/pkg/features"
)

func init() {
	ConformanceTests = append(ConformanceTests, TelemetryPolicyTargetNotFound)
}

var TelemetryPolicyTargetNotFound = confsuite.ConformanceTest{
	ShortName:   "TelemetryPolicyTargetNotFound",
	Description: "An XTelemetryPolicy whose targetRef points to a Gateway that does not exist should have an Accepted status condition set to False with reason TargetNotFound.",
	Features: []features.FeatureName{
		features.SupportGateway,
		features.SupportTelemetryPolicy,
	},
	Manifests: []string{"tests/telemetrypolicy-target-not-found.yaml"},
	Parallel:  true,
	// Provisional: GEP-713 does not pin down which ancestorRef carries the
	// TargetNotFound condition when the targeted resource does not exist. This
	// test expects an ancestor entry for the missing Gateway itself.
	Provisional: true,
	Test: func(t *testing.T, suite *confsuite.ConformanceTestSuite) {
		ns := confsuite.InfrastructureNamespace
		gwNN := types.NamespacedName{Name: "gateway-telemetrypolicy-target-not-found", Namespace: ns}
		missingGwNN := types.NamespacedName{Name: "nonexistent-gateway", Namespace: ns}
		policyNN := types.NamespacedName{Name: "target-not-found", Namespace: ns}

		kubernetes.NamespacesMustBeReady(t, suite.Client, suite.TimeoutConfig, []string{ns})
		kubernetes.GatewayMustHaveCondition(t, suite.Client, suite.TimeoutConfig, gwNN, metav1.Condition{
			Type:   string(gatewayv1.GatewayConditionAccepted),
			Status: metav1.ConditionTrue,
			Reason: "", // any reason
		})

		t.Run("XTelemetryPolicy targeting a nonexistent Gateway has an Accepted Condition with status False and Reason TargetNotFound", func(t *testing.T) {
			targetNotFoundCond := metav1.Condition{
				Type:   string(gatewayv1.PolicyConditionAccepted),
				Status: metav1.ConditionFalse,
				Reason: string(gatewayv1.PolicyReasonTargetNotFound),
			}

			kubernetes.TelemetryPolicyMustHaveCondition(t, suite.Client, suite.TimeoutConfig, policyNN, missingGwNN, targetNotFoundCond)
		})
	},
}
