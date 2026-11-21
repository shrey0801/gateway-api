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

package features

import "k8s.io/apimachinery/pkg/util/sets"

// -----------------------------------------------------------------------------
// Features - TelemetryPolicy Conformance (Core)
// -----------------------------------------------------------------------------

const (
	// This option indicates support for TelemetryPolicy (the XTelemetryPolicy
	// resource, its targetRefs to Gateways and its status).
	SupportTelemetryPolicy FeatureName = "TelemetryPolicy"
)

// TelemetryPolicyFeature contains metadata for the TelemetryPolicy feature.
var TelemetryPolicyFeature = Feature{
	Name:    SupportTelemetryPolicy,
	Channel: FeatureChannelExperimental,
}

// TelemetryPolicyCoreFeatures includes all the supported features for the
// TelemetryPolicy API at a Core level of support.
var TelemetryPolicyCoreFeatures = sets.New(
	TelemetryPolicyFeature,
)

// -----------------------------------------------------------------------------
// Features - TelemetryPolicy Conformance (Extended)
// -----------------------------------------------------------------------------

const (
	// This option indicates support for the tracing configuration of
	// TelemetryPolicy (mode, provider, samplingRate, serviceName, spanName and
	// attributes sourced from headers or literals).
	SupportTelemetryPolicyTracing FeatureName = "TelemetryPolicyTracing"

	// This option indicates support for TelemetryPolicy tracing attributes with
	// sourceType Attribute (OpenTelemetry semantic convention keys).
	SupportTelemetryPolicyAttribute FeatureName = "TelemetryPolicyAttribute"

	// This option indicates support for TelemetryPolicy parent-based sampling.
	SupportTelemetryPolicyParentBasedSampling FeatureName = "TelemetryPolicyParentBasedSampling"
)

var (
	// TelemetryPolicyTracingFeature contains metadata for the
	// TelemetryPolicyTracing feature.
	TelemetryPolicyTracingFeature = Feature{
		Name:    SupportTelemetryPolicyTracing,
		Channel: FeatureChannelExperimental,
	}

	// TelemetryPolicyAttributeFeature contains metadata for the
	// TelemetryPolicyAttribute feature.
	TelemetryPolicyAttributeFeature = Feature{
		Name:    SupportTelemetryPolicyAttribute,
		Channel: FeatureChannelExperimental,
	}

	// TelemetryPolicyParentBasedSamplingFeature contains metadata for the
	// TelemetryPolicyParentBasedSampling feature.
	TelemetryPolicyParentBasedSamplingFeature = Feature{
		Name:    SupportTelemetryPolicyParentBasedSampling,
		Channel: FeatureChannelExperimental,
	}
)

// TelemetryPolicyExtendedFeatures includes all the supported features for the
// TelemetryPolicy API at an Extended level of support.
var TelemetryPolicyExtendedFeatures = sets.New(
	TelemetryPolicyTracingFeature,
	TelemetryPolicyAttributeFeature,
	TelemetryPolicyParentBasedSamplingFeature,
)
