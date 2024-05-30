// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package calculator

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/cluster"
	"gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

const CLUSTER_FEE = 0.1

type PricingService struct {
	AutopilotPricing AutopilotPriceList
	GCEPricing       GCEPriceList
	Config           *ini.File
	clientset        *kubernetes.Clientset
	metricsClientset *metricsv.Clientset
}

func NewService(sku map[string]string, region string, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, config *ini.File) (*PricingService, error) {
	apPricing, err := GetAutopilotPricing(sku["autopilot"], region)
	if err != nil {
		return nil, err
	}

	gcePricing, err := GetGCEPricing(sku["gce"], region)
	if err != nil {
		return nil, err
	}

	service := &PricingService{
		AutopilotPricing: apPricing,
		GCEPricing:       gcePricing,
		clientset:        clientset,
		metricsClientset: metricsClientset,
		Config:           config,
	}

	return service, nil
}

func (service *PricingService) CalculatePricing(cpu int64, memory int64, storage int64, gpu int64, gpuModel string, class cluster.ComputeClass, instanceType string, spot bool) float64 {
	// If spot, calculations are done based on spot pricing
	if spot {
		switch class {
		case cluster.ComputeClassPerformance:
			perfPrice := service.AutopilotPricing.SpotPerformanceCpuPricePremium*float64(cpu)/1000 + service.AutopilotPricing.SpotPerformanceMemoryPricePremium*float64(memory)/1000 + service.AutopilotPricing.SpotPerformanceLocalSSDPricePremium*float64(storage)/1000
			if perfPrice == 0 {
				log.Printf("Requested Spot Performance (%s) pricing is not available in %s region.", instanceType, service.AutopilotPricing.Region)
			}

			gcePrice, _ := service.GetGCEMachinePrice(instanceType, spot)

			return perfPrice + gcePrice
		case cluster.ComputeClassAccelerator:
			// TODO lookup machine type and add to the price
			acceleratorPrice := service.AutopilotPricing.SpotAcceleratorCpuPricePremium*float64(cpu)/1000 + service.AutopilotPricing.SpotAcceleratorMemoryGPUPricePremium*float64(memory)/1000 + service.AutopilotPricing.AcceleratorLocalSSDPricePremium*float64(storage)/1000
			switch gpuModel {
			case "nvidia-tesla-t4":
				acceleratorPrice += service.AutopilotPricing.SpotAcceleratorT4GPUPricePremium * float64(gpu)
			case "nvidia-l4":
				acceleratorPrice += service.AutopilotPricing.SpotAcceleratorL4GPUPricePremium * float64(gpu)
			case "nvidia-tesla-a100":
				acceleratorPrice += service.AutopilotPricing.SpotAcceleratorA10040GGPUPricePremium * float64(gpu)
			case "nvidia-a100-80gb":
				acceleratorPrice += service.AutopilotPricing.SpotAcceleratorA10080GGPUPricePremium * float64(gpu)
			case "nvidia-h100-80gb":
				acceleratorPrice += service.AutopilotPricing.SpotAcceleratorH100GPUPricePremium * float64(gpu)
			default:
				acceleratorPrice = 0
				log.Printf("Requested Spot GPU (%s) pricing for Accelerator compute class (%s) is not available in %s region.", gpuModel, instanceType, service.AutopilotPricing.Region)
			}

			gcePrice, _ := service.GetGCEMachinePrice(instanceType, spot)
			return acceleratorPrice + gcePrice

		case cluster.ComputeClassGPUPod:
			acceleratorPrice := service.AutopilotPricing.SpotGPUPodvCPUPrice*float64(cpu)/1000 + service.AutopilotPricing.SpotGPUPodMemoryPrice*float64(memory)/1000 + service.AutopilotPricing.SpotGPUPodLocalSSDPrice*float64(storage)/1000
			switch gpuModel {
			case "nvidia-tesla-t4":
				acceleratorPrice += service.AutopilotPricing.SpotNVIDIAT4PodGPUPrice * float64(gpu)
			case "nvidia-l4":
				acceleratorPrice += service.AutopilotPricing.SpotNVIDIAL4PodGPUPrice * float64(gpu)
			case "nvidia-tesla-a100":
				acceleratorPrice += service.AutopilotPricing.SpotNVIDIAA10040GPodGPUPrice * float64(gpu)
			case "nvidia-a100-80gb":
				acceleratorPrice += service.AutopilotPricing.SpotNVIDIAA10080GPodGPUPrice * float64(gpu)
			default:
				acceleratorPrice = 0
				log.Printf("Requested Spot GPU (%s) pricing is not available in %s region.", gpuModel, service.AutopilotPricing.Region)
			}
			return acceleratorPrice

		case cluster.ComputeClassBalanced:
			return service.AutopilotPricing.SpotCpuPrice*float64(cpu)/1000 + service.AutopilotPricing.SpotMemoryPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000

		case cluster.ComputeClassScaleout:
			return service.AutopilotPricing.SpotCpuScaleoutPrice*float64(cpu)/1000 + service.AutopilotPricing.SpotMemoryScaleoutPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000

		case cluster.ComputeClassScaleoutArm:
			armPrice := service.AutopilotPricing.SpotArmCpuScaleoutPrice*float64(cpu)/1000 + service.AutopilotPricing.SpotArmMemoryScaleoutPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
			if armPrice == 0 {
				log.Printf("Request Spot ARM (%s) pricing is not available in %s region.", instanceType, service.AutopilotPricing.Region)
			}
			return armPrice

		default:
			return service.AutopilotPricing.SpotCpuPrice*float64(cpu)/1000 + service.AutopilotPricing.SpotMemoryPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
		}
	}

	switch class {
	case cluster.ComputeClassPerformance:
		perfPrice := service.AutopilotPricing.PerformanceCpuPricePremium*float64(cpu)/1000 + service.AutopilotPricing.PerformanceMemoryPricePremium*float64(memory)/1000 + service.AutopilotPricing.PerformanceLocalSSDPricePremium*float64(storage)/1000
		if perfPrice == 0 {
			log.Printf("Requested Performance(%s) pricing is not available in %s region.", instanceType, service.AutopilotPricing.Region)
		}

		gcePrice, _ := service.GetGCEMachinePrice(instanceType, spot)
		return perfPrice + gcePrice
	case cluster.ComputeClassAccelerator:
		acceleratorPrice := service.AutopilotPricing.AcceleratorCpuPricePremium*float64(cpu)/1000 + service.AutopilotPricing.AcceleratorMemoryGPUPricePremium*float64(memory)/1000 + service.AutopilotPricing.AcceleratorLocalSSDPricePremium*float64(storage)/1000
		switch gpuModel {
		case "nvidia-tesla-t4":
			acceleratorPrice += service.AutopilotPricing.AcceleratorT4GPUPricePremium * float64(gpu)
		case "nvidia-l4":
			acceleratorPrice += service.AutopilotPricing.AcceleratorL4GPUPricePremium * float64(gpu)
		case "nvidia-tesla-a100":
			acceleratorPrice += service.AutopilotPricing.AcceleratorA10040GGPUPricePremium * float64(gpu)
		case "nvidia-a100-80gb":
			acceleratorPrice += service.AutopilotPricing.AcceleratorA10080GGPUPricePremium * float64(gpu)
		case "nvidia-h100-80gb":
			acceleratorPrice += service.AutopilotPricing.AcceleratorH100GPUPricePremium * float64(gpu)
		default:
			acceleratorPrice = 0
			log.Printf("Requested spot GPU (%s) pricing for Accelerator compute class (%s) is not available in %s region.", gpuModel, instanceType, service.AutopilotPricing.Region)
		}

		gcePrice, _ := service.GetGCEMachinePrice(instanceType, spot)

		return acceleratorPrice + gcePrice
	case cluster.ComputeClassGPUPod:
		acceleratorPrice := service.AutopilotPricing.GPUPodvCPUPrice*float64(cpu)/1000 + service.AutopilotPricing.GPUPodMemoryPrice*float64(memory)/1000 + service.AutopilotPricing.GPUPodLocalSSDPrice*float64(storage)/1000
		switch gpuModel {
		case "nvidia-tesla-t4":
			acceleratorPrice += service.AutopilotPricing.NVIDIAT4PodGPUPrice * float64(gpu)
		case "nvidia-l4":
			acceleratorPrice += service.AutopilotPricing.NVIDIAL4PodGPUPrice * float64(gpu)
		case "nvidia-tesla-a100":
			acceleratorPrice += service.AutopilotPricing.NVIDIAA10040GPodGPUPrice * float64(gpu)
		case "nvidia-a100-80gb":
			acceleratorPrice += service.AutopilotPricing.NVIDIAA10080GPodGPUPrice * float64(gpu)
		default:
			acceleratorPrice = 0
			log.Printf("Requested GPU (%s) pricing is not available in %s region.", gpuModel, service.AutopilotPricing.Region)
		}
		return acceleratorPrice
	case cluster.ComputeClassBalanced:
		return service.AutopilotPricing.CpuBalancedPrice*float64(cpu)/1000 + service.AutopilotPricing.MemoryBalancedPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
	case cluster.ComputeClassScaleout:
		return service.AutopilotPricing.CpuScaleoutPrice*float64(cpu)/1000 + service.AutopilotPricing.MemoryScaleoutPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
	case cluster.ComputeClassScaleoutArm:
		armPrice := service.AutopilotPricing.CpuArmScaleoutPrice*float64(cpu)/1000 + service.AutopilotPricing.MemoryArmScaleoutPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
		if armPrice == 0 {
			log.Printf("Request ARM (%s) pricing is not available in %s region.", instanceType, service.AutopilotPricing.Region)
		}
		return armPrice
	default:
		return service.AutopilotPricing.CpuPrice*float64(cpu)/1000 + service.AutopilotPricing.MemoryPrice*float64(memory)/1000 + service.AutopilotPricing.StoragePrice*float64(storage)/1000
	}
}

func (service *PricingService) GetGCEMachinePrice(instanceType string, spot bool) (float64, error) {

	instanceInfo := strings.Split(instanceType, "-")
	cpus, _ := strconv.Atoi(instanceInfo[2])
	ram := 0.0
	classType := instanceInfo[1]
	machineType := instanceInfo[0]

	switch classType {
	case "standard":
		ram = float64(cpus) * 4
	case "highcpu":
		ram = float64(cpus) * 2
	case "highmem":
		ram = float64(cpus) * 4
	case "highgpu":
		ram = float64(cpus) * 7.0833
	case "ultragpu":
		ram = float64(cpus) * 14.1666
	}

	ram = math.Ceil(ram)
	fmt.Printf("Parsing %s - %d %f %s %s", instanceType, cpus, ram, machineType, classType)

	if spot {
		switch machineType {
		case "a2":
			return service.GCEPricing.SpotA2CpuPrice*float64(cpus) + service.GCEPricing.SpotA2MemoryPrice*ram, nil
		case "a3":
			return service.GCEPricing.SpotA3CpuPrice*float64(cpus) + service.GCEPricing.SpotA3MemoryPrice*ram, nil
		case "g2":
			return service.GCEPricing.SpotG2DCpuPrice*float64(cpus) + service.GCEPricing.SpotG2DMemoryPrice*ram, nil
		case "h3":
			fmt.Printf("H3 Machine type is not available in Preemptible Spot format. Defaulting to a regular price.")
			return service.GCEPricing.H3CpuPrice*float64(cpus) + service.GCEPricing.H3MemoryPrice*ram, nil
		case "c2":
			return service.GCEPricing.SpotC2CpuPrice*float64(cpus) + service.GCEPricing.SpotC2MemoryPrice*ram, nil
		case "c2d":
			return service.GCEPricing.SpotC2DCpuPrice*float64(cpus) + service.GCEPricing.SpotC2DMemoryPrice*ram, nil
		default:
			fmt.Printf("GCE Machine type %s is not implemented for price querying. Only supported ones are A2, A3, G2, H3, C2 and C2D", instanceType)
		}
		return 0, nil
	}

	fmt.Printf("%#v", service.GCEPricing)

	switch machineType {
	case "a2":
		return service.GCEPricing.A2CpuPrice*float64(cpus) + service.GCEPricing.A2MemoryPrice*ram, nil
	case "a3":
		return service.GCEPricing.A3CpuPrice*float64(cpus) + service.GCEPricing.A3MemoryPrice*ram, nil
	case "g2":
		return service.GCEPricing.G2CpuPrice*float64(cpus) + service.GCEPricing.G2MemoryPrice*ram, nil
	case "h3":
		return service.GCEPricing.H3CpuPrice*float64(cpus) + service.GCEPricing.H3MemoryPrice*ram, nil
	case "c2":
		return service.GCEPricing.C2CpuPrice*float64(cpus) + service.GCEPricing.C2MemoryPrice*ram, nil
	case "c2d":
		return service.GCEPricing.C2DCpuPrice*float64(cpus) + service.GCEPricing.C2DMemoryPrice*ram, nil
	default:
		fmt.Printf("GCE Machine type %s is not implemented for price querying. Only supported ones are A2, A3, G2, H3, C2 and C2D", instanceType)
	}

	return 0, nil
}

func (service *PricingService) PopulateWorkloads(nodes map[string]cluster.Node) ([]cluster.Workload, error) {
	var workloads []cluster.Workload

	podMetricsList, err := service.metricsClientset.MetricsV1beta1().PodMetricses("").List(context.TODO(), metav1.ListOptions{FieldSelector: "metadata.namespace!=kube-system,metadata.namespace!=gke-gmp-system,metadata.namespace!=gmp-system"})
	if err != nil {
		log.Fatalf(err.Error())
	}

	for _, v := range podMetricsList.Items {
		pod, err := cluster.DescribePod(service.clientset, v.Name, v.Namespace)
		if err != nil {
			return nil, err
		}

		var cpu int64 = 0
		var memory int64 = 0
		var storage int64 = 0
		var gpu int64 = 0
		podContainerCount := 0

		gpuModel := pod.Spec.NodeSelector["cloud.google.com/gke-accelerator"]

		// Sum used resources from the Pod
		for _, container := range v.Containers {

			cpuUsage := container.Usage.Cpu().MilliValue()
			memoryUsage := container.Usage.Memory().MilliValue() / 1000000000            // Division to get MiB
			storageUsage := container.Usage.StorageEphemeral().MilliValue() / 1000000000 // Division to get MiB
			gpuUsage := int64(0)

			for _, specContainer := range pod.Spec.Containers {
				if container.Name == specContainer.Name {
					cpuRequest := specContainer.Resources.Requests[corev1.ResourceCPU]
					memoryRequest := specContainer.Resources.Requests[corev1.ResourceMemory]
					storageRequest := specContainer.Resources.Requests[corev1.ResourceStorage]
					gpuRequests := specContainer.Resources.Requests["nvidia.com/gpu"]

					// Usage is less than requests, so we set request as usage since the billing works like that
					if cpuUsage < cpuRequest.MilliValue() {
						cpuUsage = cpuRequest.MilliValue()
					}

					if memoryUsage < memoryRequest.MilliValue()/1000000000 {
						memoryUsage = memoryRequest.MilliValue() / 1000000000
					}

					if storageUsage < storageRequest.MilliValue()/1000000000 {
						storageUsage = memoryRequest.MilliValue() / 1000000000
					}

					gpuUsage = gpuRequests.Value()
				}
			}

			cpu += cpuUsage
			memory += memoryUsage
			storage += storageUsage
			gpu += gpuUsage
			podContainerCount++
		}

		// Check and modify the limits of summed workloads from the Pod
		cpu, memory, storage = ValidateAndRoundResources(cpu, memory, storage)

		computeClass := service.DecideComputeClass(
			v.Name,
			nodes[pod.Spec.NodeName].InstanceType,
			cpu,
			memory,
			gpu,
			gpuModel,
			strings.Contains(nodes[pod.Spec.NodeName].InstanceType, service.Config.Section("").Key("gce_arm64_prefix").String()),
		)

		cost := service.CalculatePricing(cpu, memory, storage, gpu, gpuModel, computeClass, nodes[pod.Spec.NodeName].InstanceType, nodes[pod.Spec.NodeName].Spot)

		workloadObject := cluster.Workload{
			Name:              v.Name,
			Containers:        podContainerCount,
			Node_name:         pod.Spec.NodeName,
			Cpu:               cpu,
			Memory:            memory,
			Storage:           storage,
			AcceleratorType:   gpuModel,
			AcceleratorAmount: gpu,
			Cost:              cost,
			ComputeClass:      computeClass,
		}

		workloads = append(workloads, workloadObject)

		if entry, ok := nodes[pod.Spec.NodeName]; ok {
			entry.Workloads = append(entry.Workloads, workloadObject)
			entry.Cost += cost
			nodes[pod.Spec.NodeName] = entry
		}

	}

	return workloads, nil

}

func (service *PricingService) DecideComputeClass(workloadName string, machineType string, mCPU int64, memory int64, gpu int64, gpuModel string, arm64 bool) cluster.ComputeClass {
	ratio := math.Ceil(float64(memory) / float64(mCPU))

	ratioRegularMin, _ := service.Config.Section("ratios").Key("generalpurpose_min").Float64()
	ratioRegularMax, _ := service.Config.Section("ratios").Key("generalpurpose_max").Float64()
	ratioBalancedMin, _ := service.Config.Section("ratios").Key("balanced_min").Float64()
	ratioBalancedMax, _ := service.Config.Section("ratios").Key("balanced_max").Float64()
	ratioScaleoutMin, _ := service.Config.Section("ratios").Key("scaleout_min").Float64()
	ratioScaleoutMax, _ := service.Config.Section("ratios").Key("scaleout_max").Float64()
	ratioPerformanceMin, _ := service.Config.Section("ratios").Key("performance_min").Float64()
	ratioPerformanceMax, _ := service.Config.Section("ratios").Key("performance_max").Float64()

	scaleoutMcpuMax, _ := service.Config.Section("limits").Key("scaleout_mcpu_max").Int64()
	scaleoutMemoryMax, _ := service.Config.Section("limits").Key("scaleout_memory_max").Int64()
	scaleoutArmMcpuMax, _ := service.Config.Section("limits").Key("scaleout_arm_mcpu_max").Int64()
	scaleoutArmMemoryMax, _ := service.Config.Section("limits").Key("scaleout_arm_memory_max").Int64()
	regularMcpuMax, _ := service.Config.Section("limits").Key("generalpurpose_mcpu_max").Int64()
	regularMemoryMax, _ := service.Config.Section("limits").Key("generalpurpose_memory_max").Int64()
	balancedMcpuMax, _ := service.Config.Section("limits").Key("balanced_mcpu_max").Int64()
	balancedMemoryMax, _ := service.Config.Section("limits").Key("balanced_mcpu_max").Int64()
	performanceMcpuMax, _ := service.Config.Section("limits").Key("performance_mcpu_max").Int64()
	performanceMemoryMax, _ := service.Config.Section("limits").Key("performance_memory_max").Int64()

	gpupodT4McpuMin, _ := service.Config.Section("limits").Key("gpupod_t4_mcpu_min").Int64()
	gpupodT4McpuMax, _ := service.Config.Section("limits").Key("gpupod_t4_mcpu_max").Int64()
	gpupodT4MemoryMin, _ := service.Config.Section("limits").Key("gpupod_t4_memory_min").Int64()
	gpupodT4MemoryMax, _ := service.Config.Section("limits").Key("gpupod_t4_memory_max").Int64()

	gpupodL4McpuMin, _ := service.Config.Section("limits").Key("gpupod_l4_mcpu_min").Int64()
	gpupodL4McpuMax, _ := service.Config.Section("limits").Key("gpupod_l4_mcpu_max").Int64()
	gpupodL4MemoryMin, _ := service.Config.Section("limits").Key("gpupod_l4_memory_min").Int64()
	gpupodL4MemoryMax, _ := service.Config.Section("limits").Key("gpupod_l4_memory_max").Int64()

	gpupodA10040McpuMin, _ := service.Config.Section("limits").Key("gpupod_a100_40_mcpu_min").Int64()
	gpupodA10040McpuMax, _ := service.Config.Section("limits").Key("gpupod_a100_40_mcpu_max").Int64()
	gpupodA10040MemoryMin, _ := service.Config.Section("limits").Key("gpupod_a100_40_memory_min").Int64()
	gpupodA10040MemoryMax, _ := service.Config.Section("limits").Key("gpupod_a100_40_memory_max").Int64()

	gpupodA10080McpuMin, _ := service.Config.Section("limits").Key("gpupod_a100_80_mcpu_min").Int64()
	gpupodA10080McpuMax, _ := service.Config.Section("limits").Key("gpupod_a100_80_mcpu_max").Int64()
	gpupodA10080MemoryMin, _ := service.Config.Section("limits").Key("gpupod_a100_80_memory_min").Int64()
	gpupodA10080MemoryMax, _ := service.Config.Section("limits").Key("gpupod_a100_80_memory_max").Int64()

	accelerator_mcpu_min, _ := service.Config.Section("limits").Key("accelerator_mcpu_min").Int64()
	accelerator_memory_min, _ := service.Config.Section("limits").Key("accelerator_memory_min").Int64()
	accelerator_h100_80_mcpu_max, _ := service.Config.Section("limits").Key("accelerator_h100_80_mcpu_max").Int64()
	accelerator_h100_80_memory_max, _ := service.Config.Section("limits").Key("accelerator_h100_80_memory_max").Int64()

	computeOptimizedMachineTypes := strings.Split(service.Config.Section("").Key("gce_compute_optimized_prefixed").String(), ",")
	for _, computeOptimizedMachineType := range computeOptimizedMachineTypes {
		if strings.Contains(machineType, computeOptimizedMachineType) {
			return cluster.ComputeClassPerformance
		}
	}

	// check if GPU is H100, then return ComputeClassAccelerator since it's the only one supporting these GPUs
	if gpuModel == service.Config.Section("").Key("nvidia_h100_identifier").String() {
		if ratio < ratioPerformanceMin || ratio > ratioPerformanceMax || mCPU > performanceMcpuMax || memory > performanceMemoryMax {
			log.Printf("Requested memory or CPU out of acceptable range for Performance compute class (%s) workload (%s).\n", machineType, workloadName)
		}

		return cluster.ComputeClassPerformance
	}

	acceleratorOptimizedMachineTypes := strings.Split(service.Config.Section("").Key("gce_accelerator_optimized_prefixed").String(), ",")
	for _, acceleratorOptimizedMachineType := range acceleratorOptimizedMachineTypes {
		if strings.Contains(machineType, acceleratorOptimizedMachineType) {
			switch gpuModel {
			case "nvidia-tesla-t4":
				if mCPU > gpupodT4McpuMax || mCPU < accelerator_mcpu_min || memory > gpupodT4MemoryMax || memory < accelerator_memory_min {
					log.Printf("Requested memory or CPU out of acceptable range for %s Accelerator compute class (%s) workload (%s).\n", machineType, gpuModel, workloadName)
				}
			case "nvidia-l4":
				if mCPU > gpupodL4McpuMax || mCPU < accelerator_mcpu_min || memory > gpupodL4MemoryMax || memory < accelerator_memory_min {
					log.Printf("Requested memory or CPU out of acceptable range for %s Accelerator compute class (%s) workload (%s).\n", machineType, gpuModel, workloadName)
				}
			case "nvidia-tesla-a100":
				if mCPU > gpupodA10040McpuMax || mCPU < accelerator_mcpu_min || memory > gpupodA10040MemoryMax || memory < accelerator_memory_min {
					log.Printf("Requested memory or CPU out of acceptable range for %s Accelerator compute class (%s) workload (%s).\n", machineType, gpuModel, workloadName)
				}
			case "nvidia-a100-80gb":
				if mCPU > gpupodA10080McpuMax || mCPU < accelerator_mcpu_min || memory > gpupodA10080MemoryMax || memory < accelerator_memory_min {
					log.Printf("Requested memory or CPU out of acceptable range for %s Accelerator compute class (%s) workload (%s).\n", machineType, gpuModel, workloadName)
				}
			case "nvidia-h100-80gb":
				if mCPU > accelerator_h100_80_mcpu_max || mCPU < accelerator_mcpu_min || memory > accelerator_h100_80_memory_max || memory < accelerator_memory_min {
					log.Printf("Requested memory or CPU out of acceptable range for %s Accelerator compute class (%s) workload (%s).\n", machineType, gpuModel, workloadName)
				}
			}

			return cluster.ComputeClassAccelerator
		}
	}

	// Ok, not an accelerator based workload nor is H100, so we can get a regular GPU Pod type
	if gpu > 0 {
		switch gpuModel {
		case "nvidia-tesla-t4":
			if mCPU > gpupodT4McpuMax || mCPU < gpupodT4McpuMin || memory > gpupodT4MemoryMax || memory < gpupodT4MemoryMin {
				log.Printf("Requested memory or CPU out of acceptable range for %s GPU workload (%s).\n", gpuModel, workloadName)
			}
		case "nvidia-l4":
			if mCPU > gpupodL4McpuMax || mCPU < gpupodL4McpuMin || memory > gpupodL4MemoryMax || memory < gpupodL4MemoryMin {
				log.Printf("Requested memory or CPU out of acceptable range for %s GPU workload (%s).\n", gpuModel, workloadName)
			}
		case "nvidia-tesla-a100":
			if mCPU > gpupodA10040McpuMax || mCPU < gpupodA10040McpuMin || memory > gpupodA10040MemoryMax || memory < gpupodA10040MemoryMin {
				log.Printf("Requested memory or CPU out of acceptable range for %s GPU workload (%s).\n", gpuModel, workloadName)
			}
		case "nvidia-a100-80gb":
			if mCPU > gpupodA10080McpuMax || mCPU < gpupodA10080McpuMin || memory > gpupodA10080MemoryMax || memory < gpupodA10080MemoryMin {
				log.Printf("Requested memory or CPU out of acceptable range for %s GPU workload (%s).\n", gpuModel, workloadName)
			}
		}
		return cluster.ComputeClassGPUPod
	}

	// ARM64 is still experimental
	if arm64 {
		if ratio < ratioScaleoutMin || ratio > ratioScaleoutMax || mCPU > scaleoutArmMcpuMax || memory > scaleoutArmMemoryMax {
			log.Printf("Requesting arm64 but requested mCPU () or memory or ratio are out of accepted range(%s).\n", workloadName)
		}

		return cluster.ComputeClassScaleoutArm
	}

	// For T2a machines, default to scale-out compute class, since it's the only one supporting it
	if ratio >= ratioRegularMin && ratio <= ratioRegularMax && mCPU <= regularMcpuMax && memory <= regularMemoryMax {
		return cluster.ComputeClassGeneralPurpose
	}

	// If we are out of Regular range, suggest Scale-Out
	if ratio >= ratioScaleoutMin && ratio <= ratioScaleoutMax && mCPU <= scaleoutMcpuMax && memory <= scaleoutMemoryMax {
		return cluster.ComputeClassScaleout
	}

	// If usage is more than general-purpose limits, default to balanced
	if ratio >= ratioBalancedMin && ratio <= ratioBalancedMax && mCPU <= balancedMcpuMax && memory <= balancedMemoryMax {
		return cluster.ComputeClassBalanced
	}

	log.Printf("Couldn't find a matching compute class for %s. Defaulting to 'General-purpose'. Please check the pricing manually.\n", workloadName)

	return cluster.ComputeClassGeneralPurpose
}

// TODO: implement ini file minimums
func ValidateAndRoundResources(mCPU int64, memory int64, storage int64) (int64, int64, int64) {
	// Lowest possible mCPU request, but this is different for DaemonSets that are not yet implemented
	if mCPU < 50 {
		mCPU = 50
	}

	// Minumum memory request, however it's 1G for Scaleout, we don't yet account for this
	if memory < 52 {
		memory = 52
	}

	if storage < 10 {
		storage = 10
	}

	mCPUMissing := (50 - (mCPU % 50))
	if mCPUMissing == 50 {
		// Nothing to do here, return original values
		return mCPU, memory, storage
	}

	// Add missing value to reach nearst 250mCPU step
	mCPU += mCPUMissing

	return mCPU, memory, storage
}
