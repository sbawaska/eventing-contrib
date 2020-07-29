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

package adapter

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/sets"
	"strconv"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	sourcesv1alpha1 "knative.dev/eventing-contrib/registry/pkg/apis/sources/v1alpha1"
	"knative.dev/eventing-contrib/registry/pkg/reconciler/source/resources"
	"knative.dev/eventing/pkg/adapter/v2"
	"knative.dev/pkg/logging"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"go.uber.org/zap"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

type Event string

var validEvents = []Event{
	"created",
	"updated",
	"deleted",
}

type envConfig struct {
	adapter.EnvConfig

	// Environment variable containing the HTTP port
	EnvPort string `envconfig:"PORT" default:"8080"`
	// Environment variable for registry
	EnvRegistryBaseUrl string `envconfig:"REGISTRY_BASE_URL" default:"docker.io"`
	// Environment variable for poll interval
	EnvPollInterval string `envconfig:"POLL_INTERVAL" default:"10"`
	// Environment variable containing information about the origin of the event
	EnvOwnerRepo string `envconfig:"REGISTRY_OWNER_REPO" required:"true"`
	// Environment variable containing information about tags to filter
	Tags *string `envconfig:"TAGS"`
}

// NewEnvConfig function reads env variables defined in envConfig structure and
// returns accessor interface
func NewEnvConfig() adapter.EnvConfigAccessor {
	return &envConfig{}
}

// registryAdapter converts incoming  events to CloudEvents
type registryAdapter struct {
	logger       *zap.SugaredLogger
	ceClient     cloudevents.Client
	k8sClient    *kubernetes.Clientset
	env          *envConfig
}

// NewAdapter returns the instance of gitHubReceiveAdapter that implements adapter.Adapter interface
func NewAdapter(ctx context.Context, processed adapter.EnvConfigAccessor, ceClient cloudevents.Client) adapter.Adapter {
	logger := logging.FromContext(ctx)
	env := processed.(*envConfig)
	k8sClient, err := getKubernetesClient()
	if err != nil {
		logger.Fatalf("could not create kubernetes client", err)
	}

	return &registryAdapter{
		logger:       logger,
		ceClient:     ceClient,
		k8sClient:    k8sClient,
		env:          env,
	}
}

func getKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return clientset, nil
}

// Start implements adapter.Adapter
func (a *registryAdapter) Start(ctx context.Context) error {
	return a.start(ctx.Done())
}

func (a *registryAdapter) start(stopCh <-chan struct{}) error {
	repositoryPath, err := a.getFullRepositoryPath()
	if err != nil {
		return err
	}
	src := cloudevents.ParseURIRef(repositoryPath)
	if src == nil {
		return fmt.Errorf("invalid registry for registry events: nil")
	}
	pollInterval, err := strconv.Atoi(a.env.EnvPollInterval)
	if err != nil {
		a.logger.Warn(fmt.Sprintf("cannot parse polling interval :%s, defaulting to 10 seconds", a.env.EnvPollInterval))
		pollInterval = 10
	}
	ticker := time.NewTicker(time.Duration(pollInterval) * time.Second)

	for {
		select {
		case <-stopCh:
			return nil
		case <-ticker.C:
			if err := a.pollRegistry(); err != nil {
				a.logger.Error("error while polling registry. terminating polling", err)
			}
		}
	}

	return nil
}

func (a *registryAdapter) getFullRepositoryPath() (string, error) {
	src := sourcesv1alpha1.RegistryEventSource(a.env.EnvRegistryBaseUrl, a.env.EnvOwnerRepo)
	if src == "" {
		return "", fmt.Errorf("invalid registry for registry events: empty")
	}
	return src, nil
}

func (a *registryAdapter) pollRegistry() error {

	cm, err := a.getOrCreateConfigMap()
	if err != nil {
		return err
	}

	repositoryPath, err := a.getFullRepositoryPath()
	if err != nil {
		return err
	}

	repo, err := name.NewRepository(repositoryPath)
	if err != nil {
		return err
	}
	l, err := remote.List(repo)
	if err != nil {
		return err
	}
	rawTags := a.env.Tags
	var tags sets.String
	if rawTags != nil {
		tags = sets.NewString(strings.Split(*rawTags, ",")...)
	}
	for _, tag := range l {
		if rawTags != nil && !tags.Has(tag) {
			continue
		}
		tagref, err := name.ParseReference(fmt.Sprintf("%s:%s", repositoryPath, tag))
		if err != nil {
			return err
		}
		desc, err := remote.Get(tagref)
		if err != nil {
			return err
		}

		imgWithTag := strings.ReplaceAll(tagref.String(), ":", "-")
		imgWithTag = strings.ReplaceAll(imgWithTag, "/", "-")

		if digest, found := cm.Data[imgWithTag]; !found {
			// created
			cm.Data[imgWithTag] = desc.Digest.String()
			err = a.sendEvent("created", desc)
			if err != nil {
				return err
			}
		} else if digest != desc.Digest.String() {
			// updated
			cm.Data[imgWithTag] = desc.Digest.String()
			err = a.sendEvent("updated", desc)
			if err != nil {
				return err
			}
		}
	}
	_, err = a.k8sClient.CoreV1().ConfigMaps(a.env.Namespace).Update(cm)
	return err
}

func (a *registryAdapter) getOrCreateConfigMap() (*v1.ConfigMap, error) {
	cmName, err := a.getConfigMapName()
	if err != nil {
		return nil, err
	}
	cm, err := a.k8sClient.CoreV1().ConfigMaps(a.env.GetNamespace()).Get(cmName, metav1.GetOptions{})

	if errors.IsNotFound(err) {
		cm, err = a.k8sClient.CoreV1().ConfigMaps(a.env.Namespace).Create(&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cmName,
				Namespace: a.env.Namespace,
				Labels:    resources.Labels(cmName),
			},
			Data: map[string]string{"hello":"world"},
		})

		if err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	return cm, nil

}

func (a *registryAdapter) getConfigMapName() (string, error) {
	repoPath, err := a.getFullRepositoryPath()
	if err != nil {
		return "", err
	}
	repoName := strings.ReplaceAll(repoPath, "/", "-")
	return fmt.Sprintf("registrysource-%s", repoName), nil
}

func (a *registryAdapter) sendEvent(eventType string, desc *remote.Descriptor) error {

	cloudEventType := sourcesv1alpha1.RegistryEventType()

	event := cloudevents.NewEvent()
	event.SetType(cloudEventType)
	event.SetSource(a.env.EnvRegistryBaseUrl)
	event.SetSubject(a.env.EnvOwnerRepo)
	overrides, err := a.env.GetCloudEventOverrides()
	if err != nil {
		return fmt.Errorf("failed to unmarshal cloudevent overrides: %w", err)
	}
	for key, value := range overrides.Extensions {
		if key == "action" {
			a.logger.Warnf("'action' is a reserved CloudEvent override key for RegistrySource, skipping value: %s", value)
		} else {
			event.SetExtension(key, value)
		}
	}
	event.SetExtension("action", eventType)

	payload := map[string]string {
		"Action": eventType,
		"ResourceURI": buildImageStrWithDigest(desc),
		"Digest": desc.Digest.String(),
		"Tag": desc.Ref.Identifier(),
	}

	a.logger.Info(fmt.Sprintf("sending cloudevent %+v: with payload:%+v", event, payload))
	if err := event.SetData(cloudevents.ApplicationJSON, payload); err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	result := a.ceClient.Send(context.Background(), event)
	if !cloudevents.IsACK(result) {
		return result
	}
	return nil
}

func buildImageStrWithDigest(desc *remote.Descriptor) string {
	repo := strings.Split(desc.Ref.String(), ":")
	return fmt.Sprintf("%s@%s", repo[0], desc.Digest.String())
}
