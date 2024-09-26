/*
Copyright 2020 The Kubernetes Authors.

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
package nodepressure

import (
	"context"
	"fmt"
	"math"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	pluginConfig "sigs.k8s.io/scheduler-plugins/apis/config"
)

// Name is the name of the plugin used in the Registry and configurations.
const Name = "NodePressure"

type NodePressure struct {
	handle         framework.Handle
	config         pluginConfig.NodePressureArgs
	minPressureStr string
}

var _ = framework.ScorePlugin(&NodePressure{})

func New(ctx context.Context, obj runtime.Object, h framework.Handle) (framework.Plugin, error) {

	klog.Infof("[NodePressure] Loading plugin config")

	args, ok := obj.(*pluginConfig.NodePressureArgs)
	var config pluginConfig.NodePressureArgs

	if !ok {
		klog.Warning("Unable to parse args properly, falling back to default config")
		config = pluginConfig.NodePressureArgs{
			MinPressure: 0,
			MaxPressure: 40,
			NodeLabel:   "huggingface.co/node-pressure",
		}
	} else {
		config = *args.DeepCopy()
	}
	minPressureStr := strconv.FormatInt(config.MinPressure, 10)

	return &NodePressure{
		handle:         h,
		config:         config,
		minPressureStr: minPressureStr,
	}, nil
}

// Name implements framework.ScorePlugin.
func (n *NodePressure) Name() string {
	return Name
}

// Score implements framework.ScorePlugin.
// TODO: use prom instead of node labels
func (n *NodePressure) Score(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (int64, *framework.Status) {
	// Take snapshotted node info
	nodeInfo, err := n.handle.SnapshotSharedLister().NodeInfos().Get(nodeName)
	if err != nil {
		return 0, framework.NewStatus(framework.Error, fmt.Sprintf("getting node %q from Snapshot: %v", nodeName, err))
	}
	labels := nodeInfo.Node().Labels
	pressureLabel := n.config.NodeLabel
	pressure, ok := labels[pressureLabel]
	if !ok {
		klog.Infof("[NodePressure] no label %s found for node %s", pressureLabel, nodeName)
		pressure = n.minPressureStr
	}
	pressure_i, err := strconv.ParseInt(pressure, 10, 64)
	if err != nil {
		klog.Warningf("[NodePressure] label %s found for node %s but cannot be converted to float64, err %v",
			pressureLabel, nodeName, err)
		pressure_i = n.config.MinPressure
	}
	score := n.config.MaxPressure - pressure_i
	klog.Infof("[NodePressure] Node %s, pressure %v, score: %v", nodeName, pressure_i, score)
	return score, nil
}

// ScoreExtensions implements framework.ScorePlugin.
func (n *NodePressure) ScoreExtensions() framework.ScoreExtensions {
	return n
}

// NormalizeScore implements framework.ScoreExtensions.
func (n *NodePressure) NormalizeScore(ctx context.Context, state *framework.CycleState, p *v1.Pod, scores framework.NodeScoreList) *framework.Status {
	var higherScore, lowerScore int64
	lowerScore = math.MaxInt64
	higherScore = math.MinInt64

	if len(scores) == 0 {
		return nil
	}

	for _, node := range scores {
		if node.Score > higherScore {
			higherScore = node.Score
		}
		if node.Score < lowerScore {
			lowerScore = node.Score
		}
	}

	srcInterval := higherScore - lowerScore
	dstInterval := framework.MaxNodeScore - framework.MinNodeScore
	for i, node := range scores {
		if srcInterval == 0 {
			scores[i].Score = framework.MaxNodeScore
		} else {
			scores[i].Score = (node.Score - lowerScore) * dstInterval / srcInterval
		}
	}
	klog.Infof("[NodePressure] Nodes final score: %v", scores)
	return nil
}
