/*
Copyright 2023 The Volcano Authors.

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

package vgpu

import (
	"fmt"
	"log"

	"github.com/NVIDIA/go-gpuallocator/gpuallocator"
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
	"volcano.sh/k8s-device-plugin/pkg/plugin/vgpu/config"
	"volcano.sh/k8s-device-plugin/pkg/plugin/vgpu/util"
)

// Constants representing the various MIG strategies
const (
	MigStrategyNone   = "none"
	MigStrategySingle = "single"
	MigStrategyMixed  = "mixed"
)

// MigStrategyResourceSet holds a set of resource names for a given MIG strategy
type MigStrategyResourceSet map[string]struct{}

// MigStrategy provides an interface for building the set of plugins required to implement a given MIG strategy
type MigStrategy interface {
	GetPlugins(cache *DeviceCache) []*NvidiaDevicePlugin
	MatchesResource(mig *nvml.Device, resource string) bool
}

// NewMigStrategy returns a reference to a given MigStrategy based on the 'strategy' passed in
func NewMigStrategy(strategy string) (MigStrategy, error) {
	switch strategy {
	case MigStrategyNone:
		return &migStrategyNone{}, nil
	case MigStrategySingle:
		return &migStrategySingle{}, nil
	case MigStrategyMixed:
		return &migStrategyMixed{}, nil
	}
	return nil, fmt.Errorf("unknown strategy: %v", strategy)
}

type (
	migStrategyNone   struct{}
	migStrategySingle struct{}
	migStrategyMixed  struct{}
)

// migStrategyNone
func (s *migStrategyNone) GetPlugins(cache *DeviceCache) []*NvidiaDevicePlugin {
	return []*NvidiaDevicePlugin{
		NewNvidiaDevicePlugin(
			//"nvidia.com/gpu",
			util.ResourceName,
			cache,
			gpuallocator.NewBestEffortPolicy(),
			pluginapi.DevicePluginPath+"nvidia-gpu.sock",
			"NVIDIA_VISIBLE_DEVICES",
			config.DeviceListStrategy,
		),
	}
}

func (s *migStrategyNone) MatchesResource(mig *nvml.Device, resource string) bool {
	panic("Should never be called")
}

// migStrategySingle
func (s *migStrategySingle) GetPlugins(cache *DeviceCache) []*NvidiaDevicePlugin {
	panic("single mode in MIG currently not supported")
}

func (s *migStrategySingle) validMigDevice(mig *nvml.Device) bool {
	attr, err := mig.GetAttributes()
	check(err)

	return attr.GpuInstanceSliceCount == attr.ComputeInstanceSliceCount
}

func (s *migStrategySingle) getResourceName(mig *nvml.Device) string {
	attr, err := mig.GetAttributes()
	check(err)

	g := attr.GpuInstanceSliceCount
	c := attr.ComputeInstanceSliceCount
	gb := ((attr.MemorySizeMB + 1024 - 1) / 1024)

	var r string
	if g == c {
		r = fmt.Sprintf("mig-%dg.%dgb", g, gb)
	} else {
		r = fmt.Sprintf("mig-%dc.%dg.%dgb", c, g, gb)
	}

	return r
}

func (s *migStrategySingle) MatchesResource(mig *nvml.Device, resource string) bool {
	return true
}

// migStrategyMixed
func (s *migStrategyMixed) GetPlugins(cache *DeviceCache) []*NvidiaDevicePlugin {
	devices := NewMIGCapableDevices()

	if err := devices.AssertAllMigEnabledDevicesAreValid(); err != nil {
		panic(fmt.Errorf("at least one device with migEnabled=true was not configured correctly: %v", err))
	}

	resources := make(MigStrategyResourceSet)
	migs, err := devices.GetAllMigDevices()
	if err != nil {
		panic(fmt.Errorf("unable to retrieve list of MIG devices: %v", err))
	}
	for _, mig := range migs {
		r := s.getResourceName(mig)
		if !s.validMigDevice(mig) {
			log.Printf("Skipping unsupported MIG device: %v", r)
			continue
		}
		resources[r] = struct{}{}
	}

	plugins := []*NvidiaDevicePlugin{
		NewNvidiaDevicePlugin(
			//"nvidia.com/gpu",
			util.ResourceName,
			cache,
			gpuallocator.NewBestEffortPolicy(),
			pluginapi.DevicePluginPath+"nvidia-gpu.sock",
			"NVIDIA_VISIBLE_DEVICES",
			config.DeviceListStrategy,
		),
	}

	for resource := range resources {
		plugin := NewMIGNvidiaDevicePlugin(
			"nvidia.com/"+resource,
			NewMigDeviceManager(s, resource),
			gpuallocator.Policy(nil),
			pluginapi.DevicePluginPath+"nvidia-"+resource+".sock",
			"NVIDIA_VISIBLE_DEVICES",
			config.DeviceListStrategy,
		)
		plugins = append(plugins, plugin)
	}

	return plugins
}

func (s *migStrategyMixed) validMigDevice(mig *nvml.Device) bool {
	attr, err := mig.GetAttributes()
	check(err)

	return attr.GpuInstanceSliceCount == attr.ComputeInstanceSliceCount
}

func (s *migStrategyMixed) getResourceName(mig *nvml.Device) string {
	attr, err := mig.GetAttributes()
	check(err)

	g := attr.GpuInstanceSliceCount
	c := attr.ComputeInstanceSliceCount
	gb := ((attr.MemorySizeMB + 1024 - 1) / 1024)

	var r string
	if g == c {
		r = fmt.Sprintf("mig-%dg.%dgb", g, gb)
	} else {
		r = fmt.Sprintf("mig-%dc.%dg.%dgb", c, g, gb)
	}

	return r
}

func (s *migStrategyMixed) MatchesResource(mig *nvml.Device, resource string) bool {
	return s.getResourceName(mig) == resource
}
