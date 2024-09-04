/*
Copyright 2020 The Volcano Authors.

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

package nvidia

import (
	"context"
	"encoding/json"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"math"
	"strconv"
	"strings"

	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	"github.com/prometheus/common/log"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// Container specific operations
func IsGPURequiredContainer(c *v1.Container, resourceName string) bool {
	return GetGPUResourceOfContainer(c, v1.ResourceName(resourceName)) > 0
}

func GetGPUResourceOfContainer(container *v1.Container, resourceName v1.ResourceName) uint {
	var count uint
	if val, ok := container.Resources.Limits[resourceName]; ok {
		count = uint(val.Value())
	}
	return count
}

// Device specific operations
var (
	gpuMemory uint
)

func GenerateVirtualDeviceID(id uint, fakeCounter uint) string {
	return fmt.Sprintf("%d-%d", id, fakeCounter)
}

func SetGPUMemory(raw uint) {
	gpuMemory = raw
	log.Infof("set gpu memory: %d", gpuMemory)
}

func GetGPUMemory() uint {
	return gpuMemory
}

// GetDevices returns virtual devices and all physical devices by index.
func GetDevices(gpuMemoryFactor uint) ([]*pluginapi.Device, map[uint]string) {
	n, err := nvml.GetDeviceCount()
	check(err)

	var virtualDevs []*pluginapi.Device
	deviceByIndex := map[uint]string{}
	for i := uint(0); i < n; i++ {
		d, err := nvml.NewDevice(i)
		check(err)
		id := i
		deviceByIndex[id] = d.UUID
		// TODO: Do we assume all cards are of same capacity
		if GetGPUMemory() == uint(0) {
			SetGPUMemory(uint(*d.Memory))
		}
		for j := uint(0); j < GetGPUMemory()/gpuMemoryFactor; j++ {
			fakeID := GenerateVirtualDeviceID(id, j)
			virtualDevs = append(virtualDevs, &pluginapi.Device{
				ID:     fakeID,
				Health: pluginapi.Healthy,
			})
		}
	}

	return virtualDevs, deviceByIndex
}

// Pod specific operations

type podSlice []*v1.Pod

func (s podSlice) Len() int {
	return len(s)
}

func (s podSlice) Less(i, j int) bool {
	return GetPredicateTimeFromPodAnnotation(s[i]) <= GetPredicateTimeFromPodAnnotation(s[j])
}

func (s podSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func IsGPURequiredPod(pod *v1.Pod, resourceName string) bool {
	return GetGPUResourceOfPod(pod, v1.ResourceName(resourceName)) > 0
}

func IsGPUAssignedPod(pod *v1.Pod) bool {

	assigned, ok := pod.ObjectMeta.Annotations[GPUAssigned]
	if !ok {
		klog.V(4).Infof("no assigned flag",
			pod.Name,
			pod.Namespace)
		return false
	}

	if assigned == "false" {
		klog.V(4).Infof("pod has not been assigned",
			pod.Name,
			pod.Namespace)
		return false
	}

	return true
}

func IsShouldDeletePod(pod *v1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return true
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil &&
			strings.Contains(status.State.Waiting.Message, "PreStartContainer check failed") {
			return true
		}
	}
	return pod.Status.Reason == "UnexpectedAdmissionError"
}

func GetGPUResourceOfPod(pod *v1.Pod, resourceName v1.ResourceName) uint {
	var total uint
	containers := pod.Spec.Containers
	for _, container := range containers {
		if val, ok := container.Resources.Limits[resourceName]; ok {
			total += uint(val.Value())
		}
	}
	return total
}

func GetPredicateTimeFromPodAnnotation(pod *v1.Pod) uint64 {
	if assumeTimeStr, ok := pod.Annotations[PredicateTime]; ok {
		predicateTime, err := strconv.ParseUint(assumeTimeStr, 10, 64)
		if err == nil {
			return predicateTime
		}
	}

	return math.MaxUint64
}

// GetGPUIDFromPodAnnotation returns the ID of the GPU if allocated
func GetGPUIDsFromPodAnnotation(pod *v1.Pod) []int {
	if len(pod.Annotations) > 0 {
		value, found := pod.Annotations[GPUIndex]
		if found {
			ids := strings.Split(value, ",")
			idSlice := make([]int, len(ids))
			for idx, id := range ids {
				j, err := strconv.Atoi(id)
				if err != nil {
					klog.Errorf("invalid %s=%s", GPUIndex, value)
					return nil
				}
				idSlice[idx] = j
			}
			return idSlice
		}
	}

	return nil
}

type objectForAddAnnotations struct {
	Metadata objectMetaForPatch `json:"metadata"`
}

type objectMetaForPatch struct {
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

func UpdatePodAnnotations(kubeClient *kubernetes.Clientset, pod *v1.Pod) error {
	pod, err := kubeClient.CoreV1().Pods(pod.Namespace).Get(context.TODO(), pod.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if len(pod.Annotations) == 0 {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[GPUAssigned] = "true"

	addAnnotationsPath := objectForAddAnnotations{
		Metadata: objectMetaForPatch{
			Annotations: pod.Annotations,
		},
	}

	patchBytes, err := json.Marshal(&addAnnotationsPath)
	if err != nil {
		return err
	}

	_, err = kubeClient.CoreV1().Pods(pod.Namespace).Patch(context.TODO(), pod.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{})
	return err
}
