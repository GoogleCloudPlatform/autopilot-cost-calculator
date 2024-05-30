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
	"strings"

	"golang.org/x/exp/slices"
	"google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/option"
)

type GCEPriceList struct {
	// generic for all
	Region string

	H3CpuPrice    float64
	H3MemoryPrice float64

	C2CpuPrice     float64
	C2MemoryPrice  float64
	C2DCpuPrice    float64
	C2DMemoryPrice float64

	G2CpuPrice    float64
	G2MemoryPrice float64
	A2CpuPrice    float64
	A2MemoryPrice float64
	A3CpuPrice    float64
	A3MemoryPrice float64

	SpotC2CpuPrice     float64
	SpotC2MemoryPrice  float64
	SpotC2DCpuPrice    float64
	SpotC2DMemoryPrice float64

	SpotG2DCpuPrice    float64
	SpotG2DMemoryPrice float64
	SpotA2CpuPrice     float64
	SpotA2MemoryPrice  float64
	SpotA3CpuPrice     float64
	SpotA3MemoryPrice  float64
}

type AutopilotPriceList struct {
	// generic for all
	Region       string
	StoragePrice float64

	// Non-specific workloads
	CpuPrice        float64
	MemoryPrice     float64
	SpotCpuPrice    float64
	SpotMemoryPrice float64

	CpuBalancedPrice        float64
	MemoryBalancedPrice     float64
	SpotCpuBalancedPrice    float64
	SpotMemoryBalancedPrice float64

	CpuScaleoutPrice        float64
	MemoryScaleoutPrice     float64
	SpotCpuScaleoutPrice    float64
	SpotMemoryScaleoutPrice float64

	CpuArmScaleoutPrice        float64
	MemoryArmScaleoutPrice     float64
	SpotArmCpuScaleoutPrice    float64
	SpotArmMemoryScaleoutPrice float64

	// gpu pricing
	GPUPodvCPUPrice              float64
	GPUPodMemoryPrice            float64
	GPUPodLocalSSDPrice          float64
	NVIDIAL4PodGPUPrice          float64
	NVIDIAT4PodGPUPrice          float64
	NVIDIAA10040GPodGPUPrice     float64
	NVIDIAA10080GPodGPUPrice     float64
	SpotGPUPodvCPUPrice          float64
	SpotGPUPodMemoryPrice        float64
	SpotGPUPodLocalSSDPrice      float64
	SpotGPUPodPDPricePremium     float64
	SpotNVIDIAL4PodGPUPrice      float64
	SpotNVIDIAT4PodGPUPrice      float64
	SpotNVIDIAA10040GPodGPUPrice float64
	SpotNVIDIAA10080GPodGPUPrice float64

	// performance tier baseline pricing
	PerformanceCpuPricePremium          float64
	PerformanceMemoryPricePremium       float64
	PerformancePDPricePremium           float64
	PerformanceLocalSSDPricePremium     float64
	SpotPerformanceCpuPricePremium      float64
	SpotPerformanceMemoryPricePremium   float64
	SpotPerformancePDPricePremium       float64
	SpotPerformanceLocalSSDPricePremium float64

	// accelerator tier baseline pricing
	AcceleratorCpuPricePremium            float64
	AcceleratorMemoryGPUPricePremium      float64
	AcceleratorPDPricePremium             float64
	AcceleratorLocalSSDPricePremium       float64
	AcceleratorT4GPUPricePremium          float64
	AcceleratorL4GPUPricePremium          float64
	AcceleratorA10040GGPUPricePremium     float64
	AcceleratorA10080GGPUPricePremium     float64
	AcceleratorH100GPUPricePremium        float64
	SpotAcceleratorCpuPricePremium        float64
	SpotAcceleratorMemoryGPUPricePremium  float64
	SpotAcceleratorPDPricePremium         float64
	SpotAcceleratorLocalSSDPricePremium   float64
	SpotAcceleratorT4GPUPricePremium      float64
	SpotAcceleratorL4GPUPricePremium      float64
	SpotAcceleratorA10040GGPUPricePremium float64
	SpotAcceleratorA10080GGPUPricePremium float64
	SpotAcceleratorH100GPUPricePremium    float64
}

func GetGCEPricing(sku string, region string) (GCEPriceList, error) {
	pricing := GCEPriceList{
		Region:         region,
		H3CpuPrice:     0,
		H3MemoryPrice:  0,
		C2CpuPrice:     0,
		C2MemoryPrice:  0,
		C2DCpuPrice:    0,
		C2DMemoryPrice: 0,

		G2CpuPrice:    0,
		G2MemoryPrice: 0,
		A2CpuPrice:    0,
		A2MemoryPrice: 0,
		A3CpuPrice:    0,
		A3MemoryPrice: 0,

		SpotC2CpuPrice:     0,
		SpotC2MemoryPrice:  0,
		SpotC2DCpuPrice:    0,
		SpotC2DMemoryPrice: 0,

		SpotG2DCpuPrice:    0,
		SpotG2DMemoryPrice: 0,
		SpotA2CpuPrice:     0,
		SpotA2MemoryPrice:  0,
		SpotA3CpuPrice:     0,
		SpotA3MemoryPrice:  0,
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
		return GCEPriceList{}, err
	}

	err = cloudbillingService.Services.Skus.List("services/"+sku).CurrencyCode("USD").Pages(ctx, func(pricingInfo *cloudbilling.ListSkusResponse) error {
		for _, sku := range pricingInfo.Skus {
			if !slices.Contains(sku.ServiceRegions, region) {
				continue
			}

			decimal := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Units * 1000000000
			mantissa := sku.PricingInfo[0].PricingExpression.TieredRates[0].UnitPrice.Nanos * int64(sku.PricingInfo[0].PricingExpression.DisplayQuantity)

			price := float64(decimal+mantissa) / 1000000000

			switch {
			case strings.HasPrefix(sku.Description, "H3 Instance Core"):
				pricing.H3CpuPrice = price
			case strings.HasPrefix(sku.Description, "H3 Instance Ram"):
				pricing.H3MemoryPrice = price

			case strings.HasPrefix(sku.Description, "Compute optimized Instance Core"):
				pricing.C2CpuPrice = price
			case strings.HasPrefix(sku.Description, "Compute optimized Instance Ram"):
				pricing.C2MemoryPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible Compute optimized Instance Core"):
				pricing.SpotC2CpuPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible Compute optimized Instance Ram"):

				pricing.SpotC2MemoryPrice = price
			case strings.HasPrefix(sku.Description, "C2D AMD Instance Core"):
				pricing.C2DCpuPrice = price
			case strings.HasPrefix(sku.Description, "C2D AMD Instance Ram"):
				pricing.C2DMemoryPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible C2D AMD Instance Core"):
				pricing.SpotC2DCpuPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible C2D AMD Instance Ram"):
				pricing.SpotC2DMemoryPrice = price

			case strings.HasPrefix(sku.Description, "G2 Instance Core"):
				pricing.G2CpuPrice = price
			case strings.HasPrefix(sku.Description, "G2 Instance Ram"):
				pricing.G2MemoryPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible G2 Instance Core"):
				pricing.SpotG2DCpuPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible G2 Instance Ram"):
				pricing.SpotG2DMemoryPrice = price

			case strings.HasPrefix(sku.Description, "A2 Instance Core"):
				pricing.A2CpuPrice = price
			case strings.HasPrefix(sku.Description, "A2 Instance Ram"):
				pricing.A2MemoryPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible A2 Instance Core"):
				pricing.SpotA2CpuPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible A2 Instance Ram"):
				pricing.SpotA2MemoryPrice = price

			case strings.HasPrefix(sku.Description, "A3 Instance Core"):
				pricing.A3CpuPrice = price
			case strings.HasPrefix(sku.Description, "A3 Instance Ram"):
				pricing.A3MemoryPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible A3 Instance Core"):
				pricing.SpotA3CpuPrice = price
			case strings.HasPrefix(sku.Description, "Spot Preemptible A3 Instance Ram"):
				pricing.SpotA3MemoryPrice = price

			}

		}

		return nil
	})

	if err != nil {
		err = fmt.Errorf("unable to fetch gce cloud billing information: %v", err)
		return GCEPriceList{}, err
	}

	return pricing, nil
}

func GetAutopilotPricing(sku string, region string) (AutopilotPriceList, error) {
	// Init all to zeroes
	pricing := AutopilotPriceList{
		Region:                     region,
		StoragePrice:               0,
		CpuPrice:                   0,
		MemoryPrice:                0,
		SpotCpuPrice:               0,
		SpotMemoryPrice:            0,
		CpuBalancedPrice:           0,
		MemoryBalancedPrice:        0,
		SpotCpuBalancedPrice:       0,
		SpotMemoryBalancedPrice:    0,
		CpuScaleoutPrice:           0,
		MemoryScaleoutPrice:        0,
		SpotCpuScaleoutPrice:       0,
		SpotMemoryScaleoutPrice:    0,
		CpuArmScaleoutPrice:        0,
		MemoryArmScaleoutPrice:     0,
		SpotArmCpuScaleoutPrice:    0,
		SpotArmMemoryScaleoutPrice: 0,

		GPUPodvCPUPrice:              0,
		GPUPodMemoryPrice:            0,
		GPUPodLocalSSDPrice:          0,
		NVIDIAL4PodGPUPrice:          0,
		NVIDIAT4PodGPUPrice:          0,
		NVIDIAA10040GPodGPUPrice:     0,
		NVIDIAA10080GPodGPUPrice:     0,
		SpotGPUPodvCPUPrice:          0,
		SpotGPUPodMemoryPrice:        0,
		SpotGPUPodLocalSSDPrice:      0,
		SpotNVIDIAL4PodGPUPrice:      0,
		SpotNVIDIAT4PodGPUPrice:      0,
		SpotNVIDIAA10040GPodGPUPrice: 0,
		SpotNVIDIAA10080GPodGPUPrice: 0,

		PerformanceCpuPricePremium:          0,
		PerformanceMemoryPricePremium:       0,
		PerformancePDPricePremium:           0,
		PerformanceLocalSSDPricePremium:     0,
		SpotPerformanceCpuPricePremium:      0,
		SpotPerformanceMemoryPricePremium:   0,
		SpotPerformancePDPricePremium:       0,
		SpotPerformanceLocalSSDPricePremium: 0,

		AcceleratorCpuPricePremium:            0,
		AcceleratorMemoryGPUPricePremium:      0,
		AcceleratorPDPricePremium:             0,
		AcceleratorLocalSSDPricePremium:       0,
		AcceleratorT4GPUPricePremium:          0,
		AcceleratorL4GPUPricePremium:          0,
		AcceleratorA10040GGPUPricePremium:     0,
		AcceleratorA10080GGPUPricePremium:     0,
		AcceleratorH100GPUPricePremium:        0,
		SpotAcceleratorCpuPricePremium:        0,
		SpotAcceleratorMemoryGPUPricePremium:  0,
		SpotAcceleratorPDPricePremium:         0,
		SpotAcceleratorLocalSSDPricePremium:   0,
		SpotAcceleratorT4GPUPricePremium:      0,
		SpotAcceleratorL4GPUPricePremium:      0,
		SpotAcceleratorA10040GGPUPricePremium: 0,
		SpotAcceleratorA10080GGPUPricePremium: 0,
		SpotAcceleratorH100GPUPricePremium:    0,
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
		return AutopilotPriceList{}, err
	}

	err = cloudbillingService.Services.Skus.List("services/"+sku).CurrencyCode("USD").Pages(ctx, func(pricingInfo *cloudbilling.ListSkusResponse) error {
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

			case "Autopilot NVIDIA T4 Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA L4 Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA A100 Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA A100 80GB Pod mCPU Requests (" + region + ")":
				pricing.GPUPodvCPUPrice = price
			case "Autopilot NVIDIA T4 Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA L4 Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA A100 Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA A100 80GB Pod Memory Requests (" + region + ")":
				pricing.GPUPodMemoryPrice = price
			case "Autopilot NVIDIA T4 Pod GPU Requests (" + region + ")":
				pricing.NVIDIAT4PodGPUPrice = price
			case "Autopilot NVIDIA L4 Pod GPU Requests (" + region + ")":
				pricing.NVIDIAL4PodGPUPrice = price
			case "Autopilot NVIDIA A100 Pod GPU Requests (" + region + ")":
				pricing.NVIDIAA10040GPodGPUPrice = price
			case "Autopilot NVIDIA A100 80GB Pod GPU Requests (" + region + ")":
				pricing.NVIDIAA10080GPodGPUPrice = price
			case "Autopilot GPU Pod Local SSD (" + region + ")":
				pricing.SpotGPUPodLocalSSDPrice = price

			case "Autopilot NVIDIA T4 Spot Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA L4 Spot Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA A100 Spot Pod mCPU Requests (" + region + ")":
			case "Autopilot NVIDIA A100 80GB Spot Pod mCPU Requests (" + region + ")":
				pricing.GPUPodvCPUPrice = price
			case "Autopilot NVIDIA T4 Spot Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA L4 Spot Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA A100 Spot Pod Memory Requests (" + region + ")":
			case "Autopilot NVIDIA A100 80GB Spot Pod Memory Requests (" + region + ")":
				pricing.GPUPodMemoryPrice = price
			case "Autopilot NVIDIA T4 Spot Pod GPU Requests (" + region + ")":
				pricing.NVIDIAT4PodGPUPrice = price
			case "Autopilot NVIDIA L4 Spot Pod GPU Requests (" + region + ")":
				pricing.NVIDIAL4PodGPUPrice = price
			case "Autopilot NVIDIA A100 Spot Pod GPU Requests (" + region + ")":
				pricing.NVIDIAA10040GPodGPUPrice = price
			case "Autopilot NVIDIA A100 80GB Spot Pod GPU Requests (" + region + ")":
				pricing.NVIDIAA10080GPodGPUPrice = price
			case "Autopilot GPU Spot Pod Local SSD (" + region + ")":
				pricing.SpotGPUPodLocalSSDPrice = price

			case "Autopilot PD Balanced Premium (" + region + ")":
				pricing.PerformancePDPricePremium = price
				pricing.SpotPerformancePDPricePremium = price
				pricing.AcceleratorPDPricePremium = price
				pricing.SpotAcceleratorPDPricePremium = price

			case "Autopilot Performance CPU Premium (" + region + ")":
				pricing.PerformanceCpuPricePremium = price
			case "Autopilot Performance Memory Premium (" + region + ")":
				pricing.PerformanceMemoryPricePremium = price
			case "Autopilot Local SSD Premium (" + region + ")":
				pricing.PerformanceLocalSSDPricePremium = price
				pricing.AcceleratorLocalSSDPricePremium = price

			case "Autopilot Spot PD Balanced Premium (" + region + ")":
				pricing.PerformancePDPricePremium = price
				pricing.SpotPerformancePDPricePremium = price
				pricing.AcceleratorPDPricePremium = price
				pricing.SpotAcceleratorPDPricePremium = price

			case "Autopilot Performance Spot CPU Premium (" + region + ")":
				pricing.SpotPerformanceCpuPricePremium = price
			case "Autopilot Performance Spot Memory Premium (" + region + ")":
				pricing.SpotPerformanceMemoryPricePremium = price
			case "Autopilot Local SSD Spot Premium (" + region + ")":
				pricing.SpotPerformanceLocalSSDPricePremium = price
				pricing.SpotAcceleratorLocalSSDPricePremium = price

			case "Autopilot Accelerator CPU Premium (" + region + ")":
				pricing.AcceleratorCpuPricePremium = price
			case "Autopilot Accelerator Memory Premium (" + region + ")":
				pricing.AcceleratorMemoryGPUPricePremium = price
			case "Autopilot T4 Premium (" + region + ")":
				pricing.AcceleratorT4GPUPricePremium = price
			case "Autopilot L4 Premium (" + region + ")":
				pricing.AcceleratorL4GPUPricePremium = price
			case "Autopilot A100 40GB Premium (" + region + ")":
				pricing.AcceleratorA10040GGPUPricePremium = price
			case "Autopilot A100 80GB Premium (" + region + ")":
				pricing.AcceleratorA10080GGPUPricePremium = price
			case "Autopilot H100 80GB Premium (" + region + ")":
				pricing.AcceleratorH100GPUPricePremium = price

			case "Autopilot Accelerator Spot CPU Premium (" + region + ")":
				pricing.SpotAcceleratorCpuPricePremium = price
			case "Autopilot Accelerator Spot Memory Premium (" + region + ")":
				pricing.SpotAcceleratorMemoryGPUPricePremium = price
			case "Autopilot T4 Spot Premium (" + region + ")":
				pricing.SpotAcceleratorT4GPUPricePremium = price
			case "Autopilot L4 Spot Premium (" + region + ")":
				pricing.SpotAcceleratorL4GPUPricePremium = price
			case "Autopilot A100 40GB Spot Premium (" + region + ")":
				pricing.SpotAcceleratorA10040GGPUPricePremium = price
			case "Autopilot A100 80GB Spot Premium (" + region + ")":
				pricing.SpotAcceleratorA10080GGPUPricePremium = price
			case "Autopilot H100 80GB Spot Premium (" + region + ")":
				pricing.SpotAcceleratorH100GPUPricePremium = price
			}
		}
		return nil
	})

	if err != nil {
		err = fmt.Errorf("unable to fetch autopilot cloud billing information: %v", err)
		return AutopilotPriceList{}, err
	}

	return pricing, nil
}
