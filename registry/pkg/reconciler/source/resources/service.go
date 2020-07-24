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

package resources

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/kmeta"

	sourcesv1alpha1 "knative.dev/eventing-contrib/registry/pkg/apis/sources/v1alpha1"
)

type ServiceArgs struct {
	ReceiveAdapterImage string
	Source              *sourcesv1alpha1.RegistrySource
}

// MakeDeployment generates, but does not create, a Service for the given
// RegistrySource.
//func MakeDeployment(source *sourcesv1alpha1.RegistrySource, receiveAdapterImage string) *servingv1alpha1.Service {
func MakeDeployment(args *ServiceArgs) *appsv1.Deployment {
	labels := map[string]string{
		"receive-adapter": "registry",
	}
	sinkURI := args.Source.Status.SinkURI
	env := []corev1.EnvVar{{
		Name:  "K_SINK",
		Value: sinkURI.String(),
	}, {
		Name:  "REGISTRY_OWNER_REPO",
		Value: args.Source.Spec.OwnerAndRepository,
	}, {
		Name:  "REGISTRY_BASE_URL",
		Value: args.Source.Spec.RegistryBaseURL,
	}, {
		Name:  "POLL_INTERVAL",
		Value: args.Source.Spec.PollInterval,
	}, {
		Name:  "NAMESPACE",
		Value: args.Source.Namespace,
	}, {
		Name:  "METRICS_DOMAIN",
		Value: "knative.dev/eventing",
	}, {
		Name:  "K_METRICS_CONFIG",
		Value: "",
	}, {
		Name:  "K_LOGGING_CONFIG",
		Value: "",
	}}
	tags := args.Source.Spec.Tags
	if tags != nil {
		env = append(env, corev1.EnvVar{
			Name: "TAGS",
			Value: strings.Join(tags, ","),
		})
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-", args.Source.Name),
			Namespace:    args.Source.Namespace,
			Labels:       labels,
			OwnerReferences: []metav1.OwnerReference{
				*kmeta.NewControllerRef(args.Source),
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "registry-poller",
						Image: args.ReceiveAdapterImage,
						Env:   env,
					}},
				},
			},
		},
	}
}
