package strategy

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/api/v1"

	buildapi "github.com/openshift/origin/pkg/build/apis/build"
	"github.com/openshift/origin/pkg/build/util"
)

// DockerBuildStrategy creates a Docker build using a Docker builder image.
type DockerBuildStrategy struct {
	Image string
	// Codec is the codec to use for encoding the output pod.
	// IMPORTANT: This may break backwards compatibility when
	// it changes.
	Codec runtime.Codec
}

// CreateBuildPod creates the pod to be used for the Docker build
// TODO: Make the Pod definition configurable
func (bs *DockerBuildStrategy) CreateBuildPod(build *buildapi.Build) (*v1.Pod, error) {
	data, err := runtime.Encode(bs.Codec, build)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the build: %v", err)
	}

	privileged := true
	strategy := build.Spec.Strategy.DockerStrategy

	containerEnv := []v1.EnvVar{
		{Name: "BUILD", Value: string(data)},
	}

	addSourceEnvVars(build.Spec.Source, &containerEnv)
	addOriginVersionVar(&containerEnv)

	if len(strategy.Env) > 0 {
		util.MergeTrustedEnvWithoutDuplicates(util.CopyApiEnvVarToV1EnvVar(strategy.Env), &containerEnv, true)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildapi.GetBuildPodName(build),
			Namespace: build.Namespace,
			Labels:    getPodLabels(build),
		},
		Spec: v1.PodSpec{
			ServiceAccountName: build.Spec.ServiceAccount,
			Containers: []v1.Container{
				{
					Name:  "docker-build",
					Image: bs.Image,
					Env:   containerEnv,
					Args:  []string{},
					// TODO: run unprivileged https://github.com/openshift/origin/issues/662
					SecurityContext: &v1.SecurityContext{
						Privileged: &privileged,
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
			NodeSelector:  build.Spec.NodeSelector,
		},
	}
	pod.Spec.Containers[0].ImagePullPolicy = v1.PullIfNotPresent
	pod.Spec.Containers[0].Resources = util.CopyApiResourcesToV1Resources(&build.Spec.Resources)

	if build.Spec.CompletionDeadlineSeconds != nil {
		pod.Spec.ActiveDeadlineSeconds = build.Spec.CompletionDeadlineSeconds
	}
	if build.Spec.Source.Binary != nil {
		pod.Spec.Containers[0].Stdin = true
		pod.Spec.Containers[0].StdinOnce = true
	}

	setOwnerReference(pod, build)
	setupDockerSocket(pod)
	setupDockerSecrets(pod, build.Spec.Output.PushSecret, strategy.PullSecret, build.Spec.Source.Images)
	setupSourceSecrets(pod, build.Spec.Source.SourceSecret)
	setupSecrets(pod, build.Spec.Source.Secrets)

	return pod, nil
}
