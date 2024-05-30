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
	"fmt"
	"os"
	"strconv"

	"github.com/GoogleCloudPlatform/autopilot-cost-calculator/cluster"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	baseStyle      = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
	pinkTextStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("225")).Background(lipgloss.Color("128"))
	blueTextStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("225")).Background(lipgloss.Color("32"))
	redTextStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("225")).Background(lipgloss.Color("160"))
	greenTextStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("25")).Background(lipgloss.Color("192"))
)

type tableModel struct {
	table table.Model
}

func (m tableModel) Init() tea.Cmd { return nil }

func (m tableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Quit right away after drawing
	return m, tea.Quit
}

func (m tableModel) View() string {
	return baseStyle.Render(m.table.View()) + "\n"
}

func DisplayNodeTable(nodes map[string]cluster.Node) {
	columns := []table.Column{
		{Title: "Name", Width: 55},
		{Title: "Type", Width: 15},
		{Title: "Region", Width: 20},
		{Title: "Accelerator", Width: 25},
		{Title: "Spot?", Width: 10},
	}

	var rows []table.Row
	for _, node := range nodes {
		rows = append(rows, table.Row{node.Name, node.InstanceType, node.Region, node.Accelerator, strconv.FormatBool(node.Spot)})
	}

	tbl := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(len(rows)),
	)

	stl := table.DefaultStyles()
	stl.Header = stl.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("255")).
		BorderBottom(true).
		Bold(false)
	stl.Selected = stl.Selected.
		Foreground(lipgloss.Color("255")).
		//	Background(lipgloss.Color("57")).
		Bold(false)
	tbl.SetStyles(stl)

	program := tea.NewProgram(tableModel{tbl})
	_, err := program.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func DisplayWorkloadTable(nodes map[string]cluster.Node, oneYearDiscount float64, threeYearDiscount float64, clusterFee float64) {
	columns := []table.Column{
		{Title: "Node", Width: 55},
		{Title: "Workload", Width: 40},
		{Title: "Containers", Width: 10},
		{Title: "Spot", Width: 10},
		{Title: "mCPU", Width: 10},
		{Title: "Memory MiB", Width: 10},
		{Title: "Storage MiB", Width: 12},
		{Title: "Compute Class", Width: 13},
		{Title: "Price $/H", Width: 10},
	}

	var rows []table.Row
	totalCost := 0.0 // Cluster fee is fixed amount
	totalCostSpot := 0.0

	for _, node := range nodes {
		for _, workload := range node.Workloads {
			// Nodes on spot don't amount for 1 or 3 year commit discounts
			if node.Spot {
				totalCostSpot += workload.Cost
			} else {
				totalCost += workload.Cost
			}
			rows = append(rows,
				table.Row{
					node.Name,
					workload.Name,
					strconv.Itoa(workload.Containers),
					strconv.FormatBool(node.Spot),
					strconv.FormatInt(workload.Cpu, 10),
					strconv.FormatInt(workload.Memory, 10),
					strconv.FormatInt(workload.Storage, 10),
					cluster.ComputeClasses[workload.ComputeClass],
					strconv.FormatFloat(workload.Cost, 'G', 7, 64),
				},
			)
		}
	}

	rows = append(rows, table.Row{"Total cost per cluster per hour", "", "", "", "", "", "", "", strconv.FormatFloat(totalCost+clusterFee, 'G', 7, 64)})
	rows = append(rows, table.Row{"... 1 year commit", "", "", "", "", "", "", "", strconv.FormatFloat((totalCostSpot+totalCost*oneYearDiscount)+clusterFee, 'G', 7, 64)})
	rows = append(rows, table.Row{"... with 3 year commit", "", "", "", "", "", "", "", strconv.FormatFloat((totalCostSpot+totalCost*threeYearDiscount)+clusterFee, 'G', 7, 64)})

	tbl := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(len(rows)),
	)

	stl := table.DefaultStyles()

	stl.Header = stl.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("255")).
		BorderBottom(true).
		Bold(false)
	stl.Selected = stl.Selected.
		Foreground(lipgloss.Color("255")).
		//	Background(lipgloss.Color("57")).
		Bold(false)
	tbl.SetStyles(stl)

	program := tea.NewProgram(tableModel{tbl})
	_, err := program.Run()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
