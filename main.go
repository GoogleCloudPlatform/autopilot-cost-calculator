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
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/calculator"
	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/cluster"
	container "google.golang.org/api/container/v1"
	"gopkg.in/ini.v1"
	"k8s.io/client-go/kubernetes"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

func main() {
	cfg, err := ini.Load("config.ini")
	if err != nil {
		fmt.Printf("Fail to read file: %v", err)
		os.Exit(1)
	}

	jsonFlag := flag.Bool("json", false, "Generate json file with the results")
	jsonFileFlag := flag.String("json-file", "", "json file location")
	flag.Parse()

	// Setting up kube configurations
	kubeConfig, kubeConfigPath, err := cluster.GetKubeConfig()
	if err != nil {
		log.Fatalf("Error getting kubernetes config: %v\n", err)
	}

	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatalf("Error setting kubernetes config: %v\n", err)
	}

	metricsClientset, err := metricsv.NewForConfig(kubeConfig)
	if err != nil {
		log.Fatalf("Error setting kubernetes metrics config: %v\n", err)
	}

	svc, err := container.NewService(context.Background())
	if err != nil {
		log.Fatalf("Error initializing GKE client: %v", err)
	}

	// Extract the information out of kube config file
	currentContext, err := cluster.GetCurrentContext(kubeConfigPath)
	if err != nil {
		log.Fatalf("Error getting GKE context: %v", err)
	}

	clusterName := currentContext[3]
	clusterRegion := currentContext[2]
	clusterProject := currentContext[1]
	clusterLocation := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", clusterProject, clusterRegion, clusterName)

	clusterObject, err := svc.Projects.Locations.Clusters.Get(clusterLocation).Do()
	if err != nil {
		log.Fatalf("Error getting GKE cluster information: %s, %v", clusterName, err)
	}

	if clusterObject.Autopilot != nil && clusterObject.Autopilot.Enabled {
		log.Fatalf("This is already an Autopilot cluster, `aborting`")
	}

	nodes, err := cluster.GetClusterNodes(clientset)
	if err != nil {
		log.Fatalf("Error getting cluster nodes: %v", err)
	}

	pricingSKUs := map[string]string{
		"autopilot": cfg.Section("").Key("autopilot_sku").String(),
		"gce":       cfg.Section("").Key("gce_sku").String(),
	}
	pricingService, err := calculator.NewService(pricingSKUs, clusterRegion, clientset, metricsClientset, cfg)
	if err != nil {
		log.Fatalf("Error initializing pricing service: %v", err)
	}

	workloads, err := pricingService.PopulateWorkloads(nodes)
	if err != nil {
		log.Fatalf(err.Error())
	}

	if *jsonFlag {
		contents, _ := json.MarshalIndent(nodes, "", "    ")

		if *jsonFileFlag != "" {
			jsonOutput, err := os.Create(*jsonFileFlag)
			if err != nil {
				log.Fatalf("Error creating file for json output: %s", err.Error())
			}

			_, err = jsonOutput.Write(contents)
			if err != nil {
				log.Printf("Error writing json to file: %s", err.Error())
			}
			log.Printf("JSON output saved to %s.", *jsonFileFlag)
		} else {
			fmt.Printf("%s", contents)
		}

	} else {
		fmt.Println(pinkTextStyle.Render(fmt.Sprintf("Cluster %q (%s) on version: v%s", clusterObject.Name, clusterObject.Status, clusterObject.CurrentMasterVersion)))
		fmt.Println()

		fmt.Println(blueTextStyle.Render(fmt.Sprintf("Nodes that you currently have at your cluster in %s: %d", clusterRegion, len(nodes))))
		DisplayNodeTable(nodes)
		fmt.Println()

		oneYearDiscount, err := cfg.Section("discounts").Key("oneyear_commit").Float64()
		if err != nil {
			oneYearDiscount = 1
		}
		threeYearDiscount, err := cfg.Section("discounts").Key("threeyear_commit").Float64()
		if err != nil {
			threeYearDiscount = 1
		}

		fmt.Println(greenTextStyle.Render(fmt.Sprintf("%d workloads from your cluster (%s) mapped to GKE Autopilot mode.", len(workloads), clusterName)))
		fmt.Println()
		fmt.Println(redTextStyle.Render("Displayed values for mCPU, Memory and Storage are a snapshot of this point in time. Those are not requets/limits but currently used values"))

		cluster_fee, err := cfg.Section("fees").Key("cluster_fee").Float64()
		if err != nil {
			cluster_fee = calculator.CLUSTER_FEE
		}

		DisplayWorkloadTable(nodes, oneYearDiscount, threeYearDiscount, cluster_fee)
	}
}
