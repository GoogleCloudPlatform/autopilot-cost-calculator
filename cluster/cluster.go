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

package cluster

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ComputeClass int8

const (
	ComputeClassRegular     ComputeClass = 0
	ComputeClassBalanced    ComputeClass = 1
	ComputeClassScaleout    ComputeClass = 2
	ComputeClassScaleoutArm ComputeClass = 3
)

var ComputeClasses [4]string = [4]string{"Regular", "Balanced", "Scale-out", "Scale-out arm64"}

type Workload struct {
	Name         string
	Node_name    string
	Containers   int
	Cpu          int64
	Memory       int64
	Storage      int64
	Cost         float64
	ComputeClass ComputeClass
}

type Node struct {
	Name         string
	Workloads    []Workload
	InstanceType string
	Region       string
	Spot         bool
	Cost         float64
}

func GetKubeConfig() (*rest.Config, string, error) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		err = fmt.Errorf("error getting user home dir: %v", err)
		return nil, "", err
	}

	kubeConfigPath := filepath.Join(userHomeDir, ".kube", "config")
	// log.Printf("Using kubeconfig: %s\n", kubeConfigPath)

	kubeConfig, err := clientcmd.BuildConfigFromFlags("", kubeConfigPath)
	if err != nil {
		err = fmt.Errorf("error getting kubernetes config: %v", err)
		return nil, "", err
	}

	return kubeConfig, kubeConfigPath, nil
}

func GetCurrentContext(kubeConfigPath string) ([]string, error) {
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfigPath},
		&clientcmd.ConfigOverrides{
			CurrentContext: "",
		}).RawConfig()

	if err != nil {
		err = fmt.Errorf("error getting kubernetes current context: %v", err)
		return nil, err
	}

	return strings.Split(config.CurrentContext, "_"), nil
}

func GetClusterNodes(clientset *kubernetes.Clientset) (map[string]Node, error) {
	nodes := make(map[string]Node)

	clusterNodes, err := ListNodes(clientset)
	if err != nil {
		err = fmt.Errorf("error getting nodes: %v", err)
		return nil, err
	}

	for _, clusterNode := range clusterNodes.Items {
		nodes[clusterNode.Name] = Node{
			Name:         clusterNode.Name,
			Region:       clusterNode.Labels["topology.kubernetes.io/region"],
			Spot:         clusterNode.Labels["cloud.google.com/gke-spot"] == "true",
			InstanceType: clusterNode.Labels["beta.kubernetes.io/instance-type"]}
	}

	return nodes, nil
}

func ListPods(client kubernetes.Interface) (*v1.PodList, error) {
	pods, err := client.CoreV1().Pods("").List(
		context.Background(),
		metav1.ListOptions{FieldSelector: "status.phase=Running,metadata.namespace!=kube-system,metadata.namespace!=gke-gmp-system"},
	)
	if err != nil {
		// Log the error, but continue execution
		log.Printf("Error listing pods: %v", err)
	}
	return pods, nil
}

func ListNamespaces(client kubernetes.Interface) (*v1.NamespaceList, error) {
	namespaces, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting namespaces: %v", err)
		return nil, err
	}
	return namespaces, nil
}

func ListNodes(client kubernetes.Interface) (*v1.NodeList, error) {
	nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		err = fmt.Errorf("error getting namespaces: %v", err)
		return nil, err
	}
	return nodes, nil
}

func DescribePod(client kubernetes.Interface, podName string, namespace string) (*v1.Pod, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		// Log the error, but continue execution
		log.Printf("Error getting pod %s in namespace %s: %v", podName, namespace, err)
	}
	return pod, nil
}
