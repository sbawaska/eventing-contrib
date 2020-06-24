/*
Copyright 2020 The Knative Authors

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
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/eventing-contrib/registry"
	"knative.dev/pkg/apis"
	"knative.dev/pkg/apis/duck"
	duckv1 "knative.dev/pkg/apis/duck/v1"
	"knative.dev/pkg/tracker"
)

var sbCondSet = apis.NewLivingConditionSet()

// GetGroupVersionKind returns the GroupVersionKind.
func (s *RegistryBinding) GetGroupVersionKind() schema.GroupVersionKind {
	return SchemeGroupVersion.WithKind("RegistryBinding")
}

// GetUntypedSpec implements apis.HasSpec
func (s *RegistryBinding) GetUntypedSpec() interface{} {
	return s.Spec
}

// GetSubject implements psbinding.Bindable
func (sb *RegistryBinding) GetSubject() tracker.Reference {
	return sb.Spec.Subject
}

// GetBindingStatus implements psbinding.Bindable
func (sb *RegistryBinding) GetBindingStatus() duck.BindableStatus {
	return &sb.Status
}

// SetObservedGeneration implements psbinding.BindableStatus
func (sbs *RegistryBindingStatus) SetObservedGeneration(gen int64) {
	sbs.ObservedGeneration = gen
}

// InitializeConditions populates the RegistryBindingStatus's conditions field
// with all of its conditions configured to Unknown.
func (sbs *RegistryBindingStatus) InitializeConditions() {
	sbCondSet.Manage(sbs).InitializeConditions()
}

// MarkBindingUnavailable marks the RegistryBinding's Ready condition to False with
// the provided reason and message.
func (sbs *RegistryBindingStatus) MarkBindingUnavailable(reason, message string) {
	sbCondSet.Manage(sbs).MarkFalse(RegistryBindingConditionReady, reason, message)
}

// MarkBindingAvailable marks the RegistryBinding's Ready condition to True.
func (sbs *RegistryBindingStatus) MarkBindingAvailable() {
	sbCondSet.Manage(sbs).MarkTrue(RegistryBindingConditionReady)
}

// Do implements psbinding.Bindable
func (sb *RegistryBinding) Do(ctx context.Context, ps *duckv1.WithPod) {
	// First undo so that we can just unconditionally append below.
	sb.Undo(ctx, ps)

}

func (sb *RegistryBinding) Undo(ctx context.Context, ps *duckv1.WithPod) {
	spec := ps.Spec.Template.Spec

	// Make sure the PodSpec does NOT have the volume.
	for i, v := range spec.Volumes {
		if v.Name == registry.VolumeName {
			ps.Spec.Template.Spec.Volumes = append(spec.Volumes[:i], spec.Volumes[i+1:]...)
			break
		}
	}

	// Make sure that none of the [init]containers have the volume mount
	for i, c := range spec.InitContainers {
		for j, vm := range c.VolumeMounts {
			if vm.Name == registry.VolumeName {
				spec.InitContainers[i].VolumeMounts = append(c.VolumeMounts[:j], c.VolumeMounts[j+1:]...)
				break
			}
		}
	}

	for i, c := range spec.Containers {
		for j, vm := range c.VolumeMounts {
			if vm.Name == registry.VolumeName {
				spec.Containers[i].VolumeMounts = append(c.VolumeMounts[:j], c.VolumeMounts[j+1:]...)
				break
			}
		}
	}
}
