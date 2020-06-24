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

package source

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	//k8s.io imports
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	//knative.dev/serving imports
	v1 "knative.dev/serving/pkg/apis/serving/v1"
	servingclientset "knative.dev/serving/pkg/client/clientset/versioned"
	servinglisters "knative.dev/serving/pkg/client/listers/serving/v1"

	//knative.dev/eventing-contrib imports
	sourcesv1alpha1 "knative.dev/eventing-contrib/registry/pkg/apis/sources/v1alpha1"
	registryreconciler "knative.dev/eventing-contrib/registry/pkg/client/injection/reconciler/sources/v1alpha1/registrysource"
	"knative.dev/eventing-contrib/registry/pkg/reconciler/source/resources"

	duckv1 "knative.dev/pkg/apis/duck/v1"

	//knative.dev/pkg imports
	"knative.dev/pkg/apis"
	"knative.dev/pkg/controller"
	pkgreconciler "knative.dev/pkg/reconciler"
	"knative.dev/pkg/resolver"
)

const (
	raImageEnvVar = "REG_RA_IMAGE"
)

// Reconciler reconciles a RegistrySource object
type Reconciler struct {
	kubeClientSet kubernetes.Interface

	servingClientSet servingclientset.Interface
	servingLister    servinglisters.ServiceLister

	receiveAdapterImage string

	sinkResolver *resolver.URIResolver
}

var _ registryreconciler.Interface = (*Reconciler)(nil)
var _ registryreconciler.Finalizer = (*Reconciler)(nil)

type webhookArgs struct {
	source                *sourcesv1alpha1.RegistrySource
	url                   *apis.URL
}

func (r *Reconciler) ReconcileKind(ctx context.Context, source *sourcesv1alpha1.RegistrySource) pkgreconciler.Event {
	source.Status.InitializeConditions()

	dest := source.Spec.Sink.DeepCopy()
	if dest.Ref != nil {
		// To call URIFromDestination(), dest.Ref must have a Namespace. If there is
		// no Namespace defined in dest.Ref, we will use the Namespace of the source
		// as the Namespace of dest.Ref.
		if dest.Ref.Namespace == "" {
			dest.Ref.Namespace = source.GetNamespace()
		}
	}

	uri, err := r.sinkResolver.URIFromDestinationV1(*dest, source)
	if err != nil {
		source.Status.MarkNoSink("NotFound", "%s", err)
		return err
	}
	source.Status.MarkSink(uri)

	ksvc, err := r.getOwnedService(ctx, source)
	if apierrors.IsNotFound(err) {
		ksvc = resources.MakeDeployment(&resources.ServiceArgs{
			Source:              source,
			ReceiveAdapterImage: r.receiveAdapterImage,
		})
		ksvc, err = r.kubeClientSet.AppsV1().Deployments(source.Namespace).Create(ksvc)
		if err != nil {
			return err
		}
		controller.GetEventRecorder(ctx).Eventf(source, corev1.EventTypeNormal, "DeploymentCreated", "Created Deployment %q", ksvc.Name)
		// TODO: Mark Deploying for the ksvc
		// Wait for the Service to get a status
	} else if err != nil {
		// Error was something other than NotFound
		return err
	} else if !metav1.IsControlledBy(ksvc, source) {
		return fmt.Errorf("Service %q is not owned by RegistrySource %q", ksvc.Name, source.Name)
	}

	source.Status.CloudEventAttributes = r.createCloudEventAttributes(source)
	source.Status.ObservedGeneration = source.Generation
	return nil
}

func (r *Reconciler) FinalizeKind(ctx context.Context, source *sourcesv1alpha1.RegistrySource) pkgreconciler.Event {
	return nil
}

func parseOwnerRepoFrom(ownerAndRepository string) (string, string, error) {
	components := strings.Split(ownerAndRepository, "/")
	if len(components) > 2 {
		return "", "", fmt.Errorf("ownerAndRepository is malformatted, expected 'owner/repository' but found %q", ownerAndRepository)
	}
	owner := components[0]
	if len(owner) == 0 && len(components) > 1 {
		return "", "", fmt.Errorf("owner is empty, expected 'owner/repository' but found %q", ownerAndRepository)
	}
	repo := ""
	if len(components) > 1 {
		repo = components[1]
	}

	return owner, repo, nil
}

func (r *Reconciler) getOwnedService(ctx context.Context, source *sourcesv1alpha1.RegistrySource) (*appsv1.Deployment, error) {
	serviceList, err := r.kubeClientSet.AppsV1().Deployments(source.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, ksvc := range serviceList.Items {
		if metav1.IsControlledBy(&ksvc, source) {
			//TODO if there are >1 controlled, delete all but first?
			return &ksvc, nil
		}
	}
	return nil, apierrors.NewNotFound(v1.Resource("services"), "")
}

func (r *Reconciler) createCloudEventAttributes(src *sourcesv1alpha1.RegistrySource) []duckv1.CloudEventAttributes {
	ceAttributes := make([]duckv1.CloudEventAttributes, 0, len(src.Spec.EventTypes))

	ceAttributes = append(ceAttributes, duckv1.CloudEventAttributes{
		Type:   sourcesv1alpha1.RegistryEventType(),
		Source: sourcesv1alpha1.RegistryEventSource(src.Spec.RegistryBaseURL, src.Spec.OwnerAndRepository),

	})

	return ceAttributes
}
