package strategy

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/admission"
	kapi "k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/v1"
	"k8s.io/kubernetes/pkg/serviceaccount"

	buildapi "github.com/openshift/origin/pkg/build/apis/build"
	"github.com/openshift/origin/pkg/build/util"
)

// SourceBuildStrategy creates STI(source to image) builds
type SourceBuildStrategy struct {
	Image string
	// Codec is the codec to use for encoding the output pod.
	// IMPORTANT: This may break backwards compatibility when
	// it changes.
	Codec            runtime.Codec
	AdmissionControl admission.Interface
}

// DefaultDropCaps is the list of capabilities to drop if the current user cannot run as root
var DefaultDropCaps = []string{
	"KILL",
	"MKNOD",
	"SETGID",
	"SETUID",
	"SYS_CHROOT",
}

// CreateBuildPod creates a pod that will execute the STI build
// TODO: Make the Pod definition configurable
func (bs *SourceBuildStrategy) CreateBuildPod(build *buildapi.Build) (*v1.Pod, error) {
	data, err := runtime.Encode(bs.Codec, build)
	if err != nil {
		return nil, fmt.Errorf("failed to encode the Build %s/%s: %v", build.Namespace, build.Name, err)
	}

	containerEnv := []v1.EnvVar{
		{Name: "BUILD", Value: string(data)},
	}

	addSourceEnvVars(build.Spec.Source, &containerEnv)
	addOriginVersionVar(&containerEnv)

	strategy := build.Spec.Strategy.SourceStrategy
	if len(strategy.Env) > 0 {
		util.MergeTrustedEnvWithoutDuplicates(util.CopyApiEnvVarToV1EnvVar(strategy.Env), &containerEnv, true)
	}

	// check if can run container as root
	if !bs.canRunAsRoot(build) {
		// TODO: both AllowedUIDs and DropCapabilities should
		// be controlled via the SCC that's in effect for the build service account
		// For now, both are hard-coded based on whether the build service account can
		// run as root.
		containerEnv = append(containerEnv, v1.EnvVar{Name: buildapi.AllowedUIDs, Value: "1-"})
		containerEnv = append(containerEnv, v1.EnvVar{Name: buildapi.DropCapabilities, Value: strings.Join(DefaultDropCaps, ",")})
	}

	privileged := true
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
					Name:  "sti-build",
					Image: bs.Image,
					Env:   containerEnv,
					// TODO: run unprivileged https://github.com/openshift/origin/issues/662
					SecurityContext: &v1.SecurityContext{
						Privileged: &privileged,
					},
					Args: []string{},
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

func (bs *SourceBuildStrategy) canRunAsRoot(build *buildapi.Build) bool {
	var rootUser int64
	rootUser = 0
	pod := &kapi.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      buildapi.GetBuildPodName(build) + "-admissioncheck",
			Namespace: build.Namespace,
		},
		Spec: kapi.PodSpec{
			ServiceAccountName: build.Spec.ServiceAccount,
			Containers: []kapi.Container{
				{
					Name:  "sti-build",
					Image: bs.Image,
					SecurityContext: &kapi.SecurityContext{
						RunAsUser: &rootUser,
					},
				},
			},
			RestartPolicy: kapi.RestartPolicyNever,
		},
	}
	userInfo := serviceaccount.UserInfo(build.Namespace, build.Spec.ServiceAccount, "")
	attrs := admission.NewAttributesRecord(pod, nil, kapi.Kind("Pod").WithVersion(""), pod.Namespace, pod.Name, kapi.Resource("pods").WithVersion(""), "", admission.Create, userInfo)
	err := bs.AdmissionControl.Admit(attrs)
	if err != nil {
		glog.V(2).Infof("Admit for root user returned error: %v", err)
	}
	return err == nil
}
