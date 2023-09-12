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
	"strings"

	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/cluster"
	"golang.org/x/exp/slices"
	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/option"
	"gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

const CLUSTER_FEE = 0.1
const ARM_TYPE_PREFIX = "t2a-"

type PriceList struct {
	// generic for all
	Region       string
	StoragePrice float64

	// regular pricing
	CpuPrice               float64
	MemoryPrice            float64
	CpuBalancedPrice       float64
	MemoryBalancedPrice    float64
	CpuScaleoutPrice       float64
	MemoryScaleoutPrice    float64
	CpuArmScaleoutPrice    float64
	MemoryArmScaleoutPrice float64

	// spot pricing
	SpotCpuPrice               float64
	SpotMemoryPrice            float64
	SpotCpuBalancedPrice       float64
	SpotMemoryBalancedPrice    float64
	SpotCpuScaleoutPrice       float64
	SpotMemoryScaleoutPrice    float64
	SpotArmCpuScaleoutPrice    float64
	SpotArmMemoryScaleoutPrice float64
}

type PricingService struct {
	Pricing          PriceList
	Config           *ini.File
	clientset        *kubernetes.Clientset
	metricsClientset *metricsv.Clientset
}

func NewService(sku string, region string, clientset *kubernetes.Clientset, metricsClientset *metricsv.Clientset, config *ini.File) (*PricingService, error) {
	pricing, err := GetAutopilotPricing(sku, region)
	if err != nil {
		return nil, err
	}

	service := &PricingService{
		Pricing:          pricing,
		clientset:        clientset,
		metricsClientset: metricsClientset,
		Config:           config,
	}

	return service, nil
}

func (service *PricingService) CalculatePricing(cpu int64, memory int64, storage int64, class cluster.ComputeClass, spot bool) float64 {
	// If spot, calculations are done based on spot pricing
	if spot {
		switch class {
		case cluster.ComputeClassBalanced:
			return service.Pricing.SpotCpuPrice*float64(cpu)/1000 + service.Pricing.SpotMemoryPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
		case cluster.ComputeClassScaleout:
			return service.Pricing.SpotCpuScaleoutPrice*float64(cpu)/1000 + service.Pricing.SpotMemoryScaleoutPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
		case cluster.ComputeClassScaleoutArm:
			if service.Pricing.SpotArmCpuScaleoutPrice == 0 || service.Pricing.SpotArmMemoryScaleoutPrice == 0 {
				log.Printf("ARM pricing is not available in this %s region.", service.Pricing.Region)
			}
			return service.Pricing.SpotArmCpuScaleoutPrice*float64(cpu)/1000 + service.Pricing.SpotArmMemoryScaleoutPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
		default:
			return service.Pricing.SpotCpuPrice*float64(cpu)/1000 + service.Pricing.SpotMemoryPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
		}
	}

	switch class {
	case cluster.ComputeClassBalanced:
		return service.Pricing.CpuBalancedPrice*float64(cpu)/1000 + service.Pricing.MemoryBalancedPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
	case cluster.ComputeClassScaleout:
		return service.Pricing.CpuScaleoutPrice*float64(cpu)/1000 + service.Pricing.MemoryScaleoutPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
	case cluster.ComputeClassScaleoutArm:
		if service.Pricing.CpuArmScaleoutPrice == 0 || service.Pricing.MemoryArmScaleoutPrice == 0 {
			log.Printf("ARM pricing is not available in this %s region.", service.Pricing.Region)
		}
		return service.Pricing.CpuArmScaleoutPrice*float64(cpu)/1000 + service.Pricing.MemoryArmScaleoutPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
	default:
		return service.Pricing.CpuPrice*float64(cpu)/1000 + service.Pricing.MemoryPrice*float64(memory)/1000 + service.Pricing.StoragePrice*float64(storage)/1000
	}
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

		if err != nil {
			return nil, err
		}

		var cpu int64 = 0
		var memory int64 = 0
		var storage int64 = 0
		podContainerCount := 0

		// Sum used resources from the Pod
		for _, container := range v.Containers {

			cpuUsage := container.Usage.Cpu().MilliValue()
			memoryUsage := container.Usage.Memory().MilliValue() / 1000000000            // Division to get MiB
			storageUsage := container.Usage.StorageEphemeral().MilliValue() / 1000000000 // Division to get MiB

			for _, specContainer := range pod.Spec.Containers {
				if container.Name == specContainer.Name {
					cpuRequest := specContainer.Resources.Requests[corev1.ResourceCPU]
					memoryRequest := specContainer.Resources.Requests[corev1.ResourceMemory]
					storageRequest := specContainer.Resources.Requests[corev1.ResourceStorage]

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
				}
			}

			cpu += cpuUsage
			memory += memoryUsage
			storage += storageUsage
			podContainerCount++
		}

		// Check and modify the limits of summed workloads from the Pod
		cpu, memory, storage = ValidateAndRoundResources(cpu, memory, storage)

		computeClass := service.DecideComputeClass(
			v.Name,
			cpu,
			memory,
			strings.Contains(nodes[pod.Spec.NodeName].InstanceType, service.Config.Section("").Key("gce_arm64_prefix").String()),
		)

		cost := service.CalculatePricing(cpu, memory, storage, computeClass, nodes[pod.Spec.NodeName].Spot)

		workloadObject := cluster.Workload{
			Name:         v.Name,
			Containers:   podContainerCount,
			Node_name:    pod.Spec.NodeName,
			Cpu:          cpu,
			Memory:       memory,
			Storage:      storage,
			Cost:         cost,
			ComputeClass: computeClass,
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

func (service *PricingService) DecideComputeClass(workloadName string, mCPU int64, memory int64, arm64 bool) cluster.ComputeClass {
	ratio := math.Ceil(float64(memory) / float64(mCPU))

	ratioRegularMin, _ := service.Config.Section("ratios").Key("regular_min").Float64()
	ratioRegularMax, _ := service.Config.Section("ratios").Key("regular_max").Float64()
	ratioBalancedMin, _ := service.Config.Section("ratios").Key("balanced_min").Float64()
	ratioBalancedMax, _ := service.Config.Section("ratios").Key("balanced_max").Float64()
	ratioScaleoutMin, _ := service.Config.Section("ratios").Key("scaleout_min").Float64()
	ratioScaleoutMax, _ := service.Config.Section("ratios").Key("scaleout_max").Float64()

	scaleoutMcpuMax, _ := service.Config.Section("limits").Key("scaleout_mcpu_max").Int64()
	scaleoutMemoryMax, _ := service.Config.Section("limits").Key("scaleout_memory_max").Int64()
	scaleoutArmMcpuMax, _ := service.Config.Section("limits").Key("scaleout_arm_mcpu_max").Int64()
	scaleoutArmMemoryMax, _ := service.Config.Section("limits").Key("scaleout_arm_memory_max").Int64()
	regularMcpuMax, _ := service.Config.Section("limits").Key("regular_mcpu_max").Int64()
	regularMemoryMax, _ := service.Config.Section("limits").Key("regular_memory_max").Int64()
	balancedMcpuMax, _ := service.Config.Section("limits").Key("balanced_mcpu_max").Int64()
	balancedMemoryMax, _ := service.Config.Section("limits").Key("balanced_mcpu_max").Int64()

	// ARM64 is still experimental
	if arm64 {
		if ratio < ratioScaleoutMin || ratio > ratioScaleoutMax || mCPU > scaleoutArmMcpuMax || memory > int64(scaleoutArmMemoryMax) {
			log.Printf("Requesting arm64 but requested mCPU () or memory or ratio are out of accepted range(%s).\n", workloadName)
		}

		return cluster.ComputeClassScaleoutArm
	}

	// For T2a machines, default to scale-out compute class, since it's the only one supporting it
	if ratio >= ratioRegularMin && ratio <= ratioRegularMax && mCPU <= regularMcpuMax && memory <= regularMemoryMax {
		return cluster.ComputeClassRegular
	}

	// If we are out of Regular range, suggest Scale-Out
	if ratio >= ratioScaleoutMin && ratio <= ratioScaleoutMax && mCPU <= scaleoutMcpuMax && memory <= scaleoutMemoryMax {
		return cluster.ComputeClassScaleout
	}

	// If usage is more than general-purpose limits, default to balanced
	if ratio >= ratioBalancedMin && ratio <= ratioBalancedMax && mCPU <= balancedMcpuMax && memory <= balancedMemoryMax {
		return cluster.ComputeClassBalanced
	}

	log.Printf("Couldn't find a matching compute class for %s. Defaulting to 'Regular'. Please check manually.\n", workloadName)

	return cluster.ComputeClassRegular
}

func GetAutopilotPricing(sku string, region string) (PriceList, error) {
	// Init all to zeroes
	pricing := PriceList{
		Region:                     region,
		StoragePrice:               0,
		CpuPrice:                   0,
		MemoryPrice:                0,
		CpuBalancedPrice:           0,
		MemoryBalancedPrice:        0,
		CpuScaleoutPrice:           0,
		MemoryScaleoutPrice:        0,
		CpuArmScaleoutPrice:        0,
		MemoryArmScaleoutPrice:     0,
		SpotCpuPrice:               0,
		SpotMemoryPrice:            0,
		SpotCpuBalancedPrice:       0,
		SpotMemoryBalancedPrice:    0,
		SpotCpuScaleoutPrice:       0,
		SpotMemoryScaleoutPrice:    0,
		SpotArmCpuScaleoutPrice:    0,
		SpotArmMemoryScaleoutPrice: 0,
	}

	// If the "region" is actual "zone", we need to remove the zone to get the pricing for the whole region.
	if len(strings.Split(region, "-")) > 2 {
		region = strings.Join(
			strings.Split(region, "-")[:len(
				strings.Split(
					region,
					"-",
				),
			)-1],
			"-",
		)
	}

	ctx := context.Background()

	cloudbillingService, err := cloudbilling.NewService(ctx, option.WithScopes(cloudbilling.CloudPlatformScope))
	if err != nil {
		err = fmt.Errorf("unable to initialize cloud billing service: %v", err)
		return PriceList{}, err
	}

	pricingInfo, err := cloudbillingService.Services.Skus.List("services/" + sku).CurrencyCode("USD").Do()
	if err != nil {
		err = fmt.Errorf("unable to fetch cloud billing prices: %v", err)
		return PriceList{}, err
	}

	for _, sku := range pricingInfo.Skus {
		if !slices.Contains(sku.ServiceRegions, region) {
			continue
		}

		decimal := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Units * 1000000000
		mantissa := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos * int64(sku.PricingInfo[0].PricingExpression.DisplayQuantity)

		price := float64(decimal+mantissa) / 1000000000

		switch sku.Description {
		case "Autopilot Pod Ephemeral Storage Requests (" + region + ")":
			pricing.StoragePrice = price

		case "Autopilot Pod Memory Requests (" + region + ")":
			pricing.MemoryPrice = price

		case "Autopilot Pod mCPU Requests (" + region + ")":
			pricing.CpuPrice = price

		case "Autopilot Balanced Pod Memory Requests (" + region + ")":
			pricing.MemoryBalancedPrice = price

		case "Autopilot Balanced Pod mCPU Requests (" + region + ")":
			pricing.CpuBalancedPrice = price

		case "Autopilot Scale-Out x86 Pod Memory Requests (" + region + ")":
			pricing.MemoryScaleoutPrice = price

		case "Autopilot Scale-Out x86 Pod mCPU Requests (" + region + ")":
			pricing.CpuScaleoutPrice = price

		case "Autopilot Scale-Out Arm Spot Pod Memory Requests (" + region + ")":
			pricing.MemoryArmScaleoutPrice = price

		case "Autopilot Scale-Out Arm Spot Pod mCPU Requests (" + region + ")":
			pricing.CpuArmScaleoutPrice = price

		case "Autopilot Spot Pod Memory Requests (" + region + ")":
			pricing.SpotMemoryPrice = price

		case "Autopilot Spot Pod mCPU Requests (" + region + ")":
			pricing.SpotCpuPrice = price

		case "Autopilot Balanced Spot Pod Memory Requests (" + region + ")":
			pricing.SpotMemoryBalancedPrice = price

		case "Autopilot Balanced Spot Pod mCPU Requests (" + region + ")":
			pricing.SpotCpuBalancedPrice = price

		case "Autopilot Scale-Out x86 Spot Pod Memory Requests (" + region + ")":
			pricing.SpotMemoryScaleoutPrice = price

		case "Autopilot Scale-Out x86 Spot Pod mCPU Requests (" + region + ")":
			pricing.SpotCpuScaleoutPrice = price

		case "Autopilot Scale-Out Arm Spot Pod Memory Requests (" + region + ")":
			pricing.SpotArmMemoryScaleoutPrice = price

		case "Autopilot Scale-Out Arm Spot Pod mCPU Requests (" + region + ")":
			pricing.SpotArmCpuScaleoutPrice = price

		}

	}

	return pricing, nil
}

func ValidateAndRoundResources(mCPU int64, memory int64, storage int64) (int64, int64, int64) {
	// Lowest possible mCPU request, but this is different for DaemonSets that are not yet implemented
	if mCPU < 250 {
		mCPU = 250
	}

	// Minumum memory request, however it's 1G for Scaleout, we don't yet account for this
	if memory < 500 {
		memory = 500
	}

	if storage < 10 {
		storage = 10
	}

	mCPUMissing := (250 - (mCPU % 250))
	if mCPUMissing == 250 {
		// Nothing to do here, return original values
		return mCPU, memory, storage
	}

	// Add missing value to reach nearst 250mCPU step
	mCPU += mCPUMissing
	if memory < mCPU {
		memory = mCPU
	}

	return mCPU, memory, storage
}
