/*
Copyright 2018 The Knative Authors

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

package v1alpha1

import (
	"fmt"

	"knative.dev/pkg/webhook/resourcesemantics"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/apis"
	duckv1 "knative.dev/pkg/apis/duck/v1"
)

// Check that RegistrySource can be validated and can be defaulted.
var _ runtime.Object = (*RegistrySource)(nil)

var _ resourcesemantics.GenericCRD = (*RegistrySource)(nil)

// RegistrySourceSpec defines the desired state of RegistrySource
// +kubebuilder:categories=all,knative,eventing,sources
type RegistrySourceSpec struct {
	// ServiceAccountName holds the name of the Kubernetes service account
	// as which the underlying K8s resources should be run. If unspecified
	// this will default to the "default" service account for the namespace
	// in which the RegistrySource exists.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// OwnerAndRepository is the registry owner/org and repository to
	// receive events from. The repository may be left off to receive
	// events from an entire organization.
	// Examples:
	//  myuser/myimage
	//  myorganization
	// +kubebuilder:validation:MinLength=1
	OwnerAndRepository string `json:"ownerAndRepository"`

	// Base url of the registry i.e. gcr.io, docker.io etc
	// Defaults to docker.io
	RegistryBaseURL string `json:"registryBaseUrl,omitempty"`

	// The polling interval in seconds. defaults to 10
	PollInterval string `json:"pollInterval,omitempty"`

	// EventType is the type of event to receive from registry.
	// +kubebuilder:validation:Enum=create,delete,update
	EventTypes []string `json:"eventTypes"`

	// Sink is a reference to an object that will resolve to a domain
	// name to use as the sink.
	// +optional
	Sink *duckv1.Destination `json:"sink,omitempty"`
}

const (
	// registryEventTypePrefix is what all Registry event types get
	// prefixed with when converting to CloudEvents.
	registryEventTypePrefix = "dev.knative.source.registry"
)

// RegistryEventType returns the Registry CloudEvent type value.
func RegistryEventType() string {
	return registryEventTypePrefix
}

// RegistryEventSource returns the GitHub CloudEvent source value.
func RegistryEventSource(registryBaseUrl, ownerAndRepo string) string {
	return fmt.Sprintf("%s/%s", registryBaseUrl, ownerAndRepo)
}

const (
	// RegistrySourceConditionReady has status True when the
	// RegistrySource is ready to send events.
	RegistrySourceConditionReady = apis.ConditionReady

	// RegistrySourceConditionSinkProvided has status True when the
	// RegistrySource has been configured with a sink target.
	RegistrySourceConditionSinkProvided apis.ConditionType = "SinkProvided"

	// GitHubServiceconditiondeployed has status True when then
	// RegistrySource Service has been deployed
	//	GitHubServiceConditionDeployed apis.ConditionType = "Deployed"

	// GitHubSourceReconciled has status True when the
	// RegistrySource has been properly reconciled
)

var registrySourceCondSet = apis.NewLivingConditionSet(
	RegistrySourceConditionSinkProvided,
)


// RegistrySourceStatus defines the observed state of RegistrySource
type RegistrySourceStatus struct {
	// inherits duck/v1 SourceStatus, which currently provides:
	// * ObservedGeneration - the 'Generation' of the Service that was last
	//   processed by the controller.
	// * Conditions - the latest available observations of a resource's current
	//   state.
	// * SinkURI - the current active sink URI that has been configured for the
	//   Source.
	duckv1.SourceStatus `json:",inline"`
}

func (s *RegistrySource) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("RegistrySource")
}

// GetCondition returns the condition currently associated with the given type, or nil.
func (s *RegistrySourceStatus) GetCondition(t apis.ConditionType) *apis.Condition {
	return registrySourceCondSet.Manage(s).GetCondition(t)
}

// IsReady returns true if the resource is ready overall.
func (s *RegistrySourceStatus) IsReady() bool {
	return registrySourceCondSet.Manage(s).IsHappy()
}

// InitializeConditions sets relevant unset conditions to Unknown state.
func (s *RegistrySourceStatus) InitializeConditions() {
	registrySourceCondSet.Manage(s).InitializeConditions()
}

// MarkSink sets the condition that the source has a sink configured.
func (s *RegistrySourceStatus) MarkSink(uri *apis.URL) {
	s.SinkURI = uri
	if uri != nil {
		registrySourceCondSet.Manage(s).MarkTrue(RegistrySourceConditionSinkProvided)
	} else {
		registrySourceCondSet.Manage(s).MarkUnknown(RegistrySourceConditionSinkProvided,
			"SinkEmpty", "Sink has resolved to empty.")
	}
}

// MarkNoSink sets the condition that the source does not have a sink configured.
func (s *RegistrySourceStatus) MarkNoSink(reason, messageFormat string, messageA ...interface{}) {
	registrySourceCondSet.Manage(s).MarkFalse(RegistrySourceConditionSinkProvided, reason, messageFormat, messageA...)
}

// MarkDeployed sets the condition that the source has been deployed.
//func (s *RegistrySourceStatus) MarkServiceDeployed(d *appsv1.Deployment) {
//	if duckv1.DeploymentIsAvailable(&d.Status, false) {
//		registrySourceCondSet.Manage(s).MarkTrue(GitHubServiceConditionDeployed)
//	} else {
//		registrySourceCondSet.Manage(s).MarkFalse(GitHubServiceConditionDeployed, "ServiceDeploymentUnavailable", "The Deployment '%s' is unavailable.", d.Name)
//	}
//}

// +genclient
// +genreconciler
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RegistrySource is the Schema for the registrysources API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:categories=all,knative,eventing,sources
type RegistrySource struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RegistrySourceSpec   `json:"spec,omitempty"`
	Status RegistrySourceStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RegistrySourceList contains a list of RegistrySource
type RegistrySourceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RegistrySource `json:"items"`
}
