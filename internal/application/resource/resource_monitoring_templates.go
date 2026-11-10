package resource

import (
	"fmt"
	"strconv"
)

type monitoringDefinition struct {
	key, label, unit, query, ageQuery, dimension string
	estimated                                    bool
	window                                       int
}

func monitoringClusterMatcher(label, value string) string {
	if label == "" {
		return ""
	}
	return label + "=" + strconv.Quote(value)
}

func monitoringSelector(name, matcher, extra string) string {
	if matcher == "" {
		if extra == "" {
			return name
		}
		return name + "{" + extra + "}"
	}
	if extra != "" {
		matcher += "," + extra
	}
	return name + "{" + matcher + "}"
}

func monitoringDefinitions(view, clusterValue, clusterLabel string) []monitoringDefinition {
	matcher := monitoringClusterMatcher(clusterLabel, clusterValue)
	clusterDimension := ""
	if clusterLabel != "" {
		clusterDimension = "," + clusterLabel
	}
	selector := func(name, extra string) string {
		return monitoringSelector(name, matcher, extra)
	}
	hostCPU := selector("node_cpu_seconds_total", `mode="idle"`)
	podCPU := selector("container_cpu_usage_seconds_total", `container!="",container!="POD",image!=""`)
	podMemory := selector("container_memory_working_set_bytes", `container!="",container!="POD",image!=""`)
	cpu := []monitoringDefinition{
		{key: "host.cpu.used", label: "主机非空闲 CPU", unit: "cores", query: "1-rate(" + hostCPU + "[5m])", ageQuery: "time()-timestamp(" + hostCPU + ")", dimension: "cpu"},
		{key: "pod.cpu.used", label: "Pod CPU 使用量", unit: "cores", query: "rate(" + podCPU + "[5m])", ageQuery: "time()-timestamp(" + podCPU + ")", dimension: "container"},
	}
	memoryTotal := selector("node_memory_MemTotal_bytes", "")
	memoryAvailable := selector("node_memory_MemAvailable_bytes", "")
	memory := []monitoringDefinition{
		{key: "host.memory.total", label: "主机内存总量", unit: "bytes", query: memoryTotal, ageQuery: "time()-timestamp(" + memoryTotal + ")"},
		{key: "host.memory.used", label: "主机内存已用（total - available）", unit: "bytes", query: memoryTotal + "-" + memoryAvailable, ageQuery: "time()-timestamp(" + memoryAvailable + ")"},
		{key: "host.memory.available", label: "主机可用内存", unit: "bytes", query: memoryAvailable, ageQuery: "time()-timestamp(" + memoryAvailable + ")"},
		{key: "pod.memory.used", label: "Pod working set", unit: "bytes", query: podMemory, ageQuery: "time()-timestamp(" + podMemory + ")", dimension: "container"},
	}
	entries := selector("node_nf_conntrack_entries", "")
	limit := selector("node_nf_conntrack_entries_limit", "")
	conntrack := []monitoringDefinition{{key: "conntrack.entries", label: "连接跟踪条目", unit: "count", query: entries, ageQuery: "time()-timestamp(" + entries + ")"}, {key: "conntrack.limit", label: "连接跟踪上限", unit: "count", query: limit, ageQuery: "time()-timestamp(" + limit + ")"}}
	for _, key := range []string{"drop", "early_drop", "insert_failed"} {
		metric := selector("node_nf_conntrack_stat_"+key, "")
		conntrack = append(conntrack, monitoringDefinition{key: "conntrack." + key + ".rate", label: key + " 速率", unit: "events/s", query: "rate(" + metric + "[5m])", ageQuery: "(count_over_time(" + metric + "[5m]) >= 5) and ((time()-timestamp(" + metric + ")) < 120)", dimension: "counter", window: 300}, monitoringDefinition{key: "conntrack." + key + ".increase", label: key + " 窗口增量（估算）", unit: "events", query: "increase(" + metric + "[5m])", ageQuery: "(count_over_time(" + metric + "[5m]) >= 5) and ((time()-timestamp(" + metric + ")) < 120)", dimension: "counter", window: 300, estimated: true})
	}
	pods := []monitoringDefinition{{key: "pods.bound", label: "已绑定非终态 Pod", unit: "count", query: fmt.Sprintf(`sum by (node%s) (max by (namespace,pod,uid,node%s) (%s) * on(namespace,pod,uid%s) group_left() max by(namespace,pod,uid%s) (%s == 1))`, clusterDimension, clusterDimension, selector("kube_pod_info", `uid!="",node!=""`), clusterDimension, clusterDimension, selector("kube_pod_status_phase", `uid!="",phase!~"Succeeded|Failed"`))}}
	switch view {
	case "cpu":
		return append(cpu, monitoringAllocationDefinitions("cpu", matcher, clusterDimension)...)
	case "memory":
		return append(memory, monitoringAllocationDefinitions("memory", matcher, clusterDimension)...)
	case "conntrack":
		return conntrack
	case "pods":
		return pods
	default:
		return []monitoringDefinition{cpu[0], memory[1], memory[0], conntrack[0], conntrack[1], pods[0]}
	}
}

func monitoringAllocationDefinitions(resource, matcher, clusterDimension string) []monitoringDefinition {
	unit := "cores"
	if resource == "memory" {
		unit = "bytes"
	}
	result := []monitoringDefinition{}
	// KSM resource gauges are historical evidence; the current API snapshot is never backfilled into these curves.
	for _, kind := range []string{"requests", "limits"} {
		metric := monitoringSelector("kube_pod_container_resource_"+kind, matcher, fmt.Sprintf(`resource=%q,uid!="",node!=""`, resource))
		query := fmt.Sprintf(`sum by (node%s) (max by (node,namespace,pod,uid,container%s) (%s))`, clusterDimension, clusterDimension, metric)
		result = append(result, monitoringDefinition{key: "pod." + resource + "." + kind, label: "已采集容器 " + kind + "（不含 Pod 级/初始化/overhead）", unit: unit, query: query, dimension: "allocation"})
	}
	metric := monitoringSelector("kube_node_status_allocatable", matcher, fmt.Sprintf(`resource=%q`, resource))
	query := fmt.Sprintf(`max by(node%s) (%s)`, clusterDimension, metric)
	return append(result, monitoringDefinition{key: "node." + resource + ".allocatable", label: "节点 allocatable 历史", unit: unit, query: query})
}
