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
	pricing calculator.PriceList
	config  *ini.File
	service calculator.PricingService
)

func TestMain(m *testing.M) {

	// Setting mocked pricing
	pricing = calculator.PriceList{
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
	}

	// Loading config
	var err error
	config, err = ini.Load("config.ini")
	if err != nil {
		log.Fatalf("Fail to read file: %v", err)
	}

	// Setting the service for the tests
	service = calculator.PricingService{
		Pricing: pricing,
		Config:  config,
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
	memoryWant = 500
	storageWant = 10

	cpu, memory, storage = calculator.ValidateAndRoundResources(249, 499, 9)
	if cpu != cpuWant || memory != memoryWant || storage != storageWant {
		t.Fatalf(`ValidateAndRoundResources(249,499,5) = %d, %d, %d doesn't match expected %d %d %d`, cpu, memory, storage, cpuWant, memoryWant, storageWant)
	}

	// Test Case #3
	cpuWant = 1750
	memoryWant = 1750
	storageWant = 900

	cpu, memory, storage = calculator.ValidateAndRoundResources(1618, 1700, 900)
	if cpu != cpuWant || memory != memoryWant || storage != storageWant {
		t.Fatalf(`ValidateAndRoundResources(1750, 1700, 900) = %d, %d, %d doesn't match expected %d %d %d`, cpu, memory, storage, cpuWant, memoryWant, storageWant)
	}

}

func TestDecideComputeClass(t *testing.T) {
	// Test Case #1
	computeClassWant := cluster.ComputeClassRegular
	computeClass := service.DecideComputeClass("test-pod", 10000, 10000, false)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(1000,1000,false) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

	// Test Case #2
	computeClassWant = cluster.ComputeClassBalanced
	computeClass = service.DecideComputeClass("test-pod", 35000, 100000, false)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(35000,100000,false) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

	// Test Case #3
	computeClassWant = cluster.ComputeClassScaleoutArm
	computeClass = service.DecideComputeClass("test-pod", 20000, 80000, true)

	if computeClass != computeClassWant {
		t.Fatalf(`DecideComputeClass(25000, 50000, true) = %s doesn't match expected %s`, cluster.ComputeClasses[computeClass], cluster.ComputeClasses[computeClassWant])
	}

}

func TestCalculatePricing(t *testing.T) {

	// Test Case #1

	computeClass := service.DecideComputeClass("test-pod", 4000, 16000, false)
	priceWant := 0.3313796 // 0.000706 (cpu price * 4) + 0.1014736 (memory price * 16) +0.2292 (storage price * 10)
	price := service.CalculatePricing(4000, 16000, 10000, computeClass, false)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

	// Test Case #2
	computeClass = service.DecideComputeClass("test-pod", 40000, 80000, false)
	priceWant = 4.0601700 // 3.324 (cpu price * 40) + 0.735464 (memory price * 80) + 0.2292 (storage price * 10)
	price = service.CalculatePricing(40000, 80000, 10000, computeClass, false)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

	// Test Case #3
	computeClass = service.DecideComputeClass("test-pod", 25000, 100000, false)
	priceWant = 0.6209660 // 0.43 (cpu spot price * 25) + 0.19026 (spot memory price * 100) + 0.000706 (spot storage price * 10)
	price = service.CalculatePricing(25000, 100000, 10000, computeClass, true)

	if !almostEqual(price, priceWant) {
		t.Fatalf(`CalculatePricing(4000, 16000, 10000, {test-region-pricing}, %s, false) = %.7f doesn't match expected %.7f`, cluster.ComputeClasses[computeClass], price, priceWant)
	}

}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) <= float64EqualityThreshold
}
