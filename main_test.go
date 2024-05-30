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

package main

import (
	"log"
	"math"
	"testing"

	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/calculator"
	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/cluster"
	"gopkg.in/ini.v1"
)

const (
	float64EqualityThreshold = 1e-9
)

var (
	autopilotPricing calculator.AutopilotPriceList
	gcePricing       calculator.GCEPriceList
	config           *ini.File
	service          calculator.PricingService
)

func TestMain(m *testing.M) {

	// Setting mocked pricing
	autopilotPricing = calculator.AutopilotPriceList{
		Region:       "test-region-1",
		StoragePrice: 0.0000706,

		// regular pricing
		CpuPrice:            0.0573,
		MemoryPrice:         0.0063421,
		CpuBalancedPrice:    0.0831,
		MemoryBalancedPrice: 0.0091933,
		CpuScaleoutPrice:    0.0722,
		MemoryScaleoutPrice: 0.0079911,

		// spot pricing
		SpotCpuPrice:            0.0172,
		SpotMemoryPrice:         0.0019026,
		SpotCpuBalancedPrice:    0.0249,
		SpotMemoryBalancedPrice: 0.002758,
		SpotCpuScaleoutPrice:    0.0217,
		SpotMemoryScaleoutPrice: 0.0023973,

		CpuArmScaleoutPrice:        0,
		MemoryArmScaleoutPrice:     0,
		SpotArmCpuScaleoutPrice:    0,
		SpotArmMemoryScaleoutPrice: 0,

		GPUPodvCPUPrice:              0.071,
		GPUPodMemoryPrice:            0,
		GPUPodLocalSSDPrice:          0,
		NVIDIAL4PodGPUPrice:          0.6783,
		NVIDIAT4PodGPUPrice:          0,
		NVIDIAA10040GPodGPUPrice:     0,
		NVIDIAA10080GPodGPUPrice:     0,
		SpotGPUPodvCPUPrice:          0.0213,
		SpotGPUPodMemoryPrice:        0,
		SpotGPUPodLocalSSDPrice:      0,
		SpotNVIDIAL4PodGPUPrice:      0,
		SpotNVIDIAT4PodGPUPrice:      0.1272,
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

	GCEPricing := calculator.GCEPriceList{
		Region:         "test-region-1",
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

	// Loading config
	var err error
	config, err = ini.Load("config.ini")
	if err != nil {
		log.Fatalf("Fail to read file: %v", err)
	}

	// Setting the service for the tests
	service = calculator.PricingService{
		AutopilotPricing: autopilotPricing,
		GCEPricing:       GCEPricing,
		Config:           config,
	}

	m.Run()
}

func TestValidateAndRoundResources(t *testing.T) {
	// Test Case #1
	var cpuWant int64 = 1000
	var memoryWant int64 = 1000
	var storageWant int64 = 1000

	cpu, memory, storage := calculator.ValidateAndRoundResources(1000, 1000, 1000)
	if cpu != cpuWant || memory != memoryWant || storage != storageWant {
		t.Fatalf(`ValidateAndRoundResources(1000,1000,1000) = %d, %d, %d doesn't match expected %d %d %d`, cpu, memory, storage, cpuWant, memoryWant, storageWant)
	}

	// Test Case #2
	cpuWant = 250
	memoryWant = 52
	storageWant = 10

	cpu, memory, storage = calculator.ValidateAndRoundResources(249, 49, 9)
	if cpu != cpuWant || memory != memoryWant || storage != storageWant {
		t.Fatalf(`ValidateAndRoundResources(249,52,5) = %d, %d, %d doesn't match expected %d %d %d`, cpu, memory, storage, cpuWant, memoryWant, storageWant)
	}

	// Test Case #3
	cpuWant = 1650
	memoryWant = 1700
	storageWant = 900

	cpu, memory, storage = calculator.ValidateAndRoundResources(1618, 1700, 900)
	if cpu != cpuWant || memory != memoryWant || storage != storageWant {
		t.Fatalf(`ValidateAndRoundResources(1650, 1700, 900) = %d, %d, %d doesn't match expected %d %d %d`, cpu, memory, storage, cpuWant, memoryWant, storageWant)
	}

}

func TestDecideComputeClass(t *testing.T) {
	// Test Case #1
	computeClassWant := cluster.ComputeClassGeneralPurpose
	computeClass := service.DecideComputeClass("test-pod", "e2-standard-4", 10000, 10000, 0, "", false)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(1000,1000,false) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

	// Test Case #2
	computeClassWant = cluster.ComputeClassBalanced
	computeClass = service.DecideComputeClass("test-pod", "e2-standard-4", 35000, 10000, 0, "", false)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(35000,100000,false) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

	// Test Case #3
	computeClassWant = cluster.ComputeClassScaleoutArm
	computeClass = service.DecideComputeClass("test-pod", "e2-standard-4", 43000, 172000, 0, "", true)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(25000, 50000, true) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

}

func TestCalculatePricing(t *testing.T) {

	// Test Case #1

	computeClass := service.DecideComputeClass("test-pod", "e2-standard-4", 4000, 16000, 0, "", false)
	priceWant := 0.3313796 // 0.000706 (cpu price * 4) + 0.1014736 (memory price * 16) +0.2292 (storage price * 10)
	price := service.CalculatePricing(4000, 16000, 10000, 0, "", computeClass, "e2-standard-4", false)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

	// Test Case #2
	computeClass = service.DecideComputeClass("test-pod", "e2-standard-4", 40000, 80000, 0, "", false)
	priceWant = 4.0601700 // 3.324 (cpu price * 40) + 0.735464 (memory price * 80) + 0.2292 (storage price * 10)
	price = service.CalculatePricing(40000, 80000, 10000, 0, "", computeClass, "e2-standard-4", false)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

	// Test Case #3
	computeClass = service.DecideComputeClass("test-pod", "e2-standard-4", 25000, 100000, 0, "", false)
	priceWant = 0.6209660 // 0.43 (cpu spot price * 25) + 0.19026 (spot memory price * 100) + 0.000706 (spot storage price * 10)
	price = service.CalculatePricing(25000, 100000, 10000, 0, "", computeClass, "e2-standard-4", true)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= float64EqualityThreshold
}
