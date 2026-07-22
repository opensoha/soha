package runtimeinfo

import (
	"runtime"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

type MetricsProvider interface {
	Snapshot() runtimeobs.Snapshot
}

type Collector struct {
	metrics MetricsProvider
	started time.Time
	mu      sync.Mutex
	last    sample
}

type sample struct {
	at        time.Time
	cpuTime   time.Duration
	networkRX int64
	networkTX int64
}

func NewCollector(metrics MetricsProvider) *Collector {
	return &Collector{metrics: metrics, started: time.Now()}
}

func (c *Collector) Snapshot() sohaapi.RuntimeResourceSnapshot {
	now := time.Now()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	cpuTime, _ := processCPUTime()
	rxBytes, txBytes, networkAvailable := networkCounters()
	cpuPercent, rxRate, txRate := c.rates(now, cpuTime, rxBytes, txBytes, networkAvailable)
	disk := diskUsage()
	heapUsage := percent(int64(memory.HeapAlloc), int64(memory.HeapSys))

	return sohaapi.RuntimeResourceSnapshot{
		GeneratedAt:   now.UTC(),
		UptimeSeconds: max(0, int64(now.Sub(c.started).Seconds())),
		CPU: sohaapi.RuntimeCPUUsage{
			LogicalCores: runtime.NumCPU(),
			UsagePercent: cpuPercent,
		},
		Memory: sohaapi.RuntimeMemoryUsage{
			GoReservedBytes:  int64(memory.Sys),
			HeapAllocBytes:   int64(memory.HeapAlloc),
			HeapSysBytes:     int64(memory.HeapSys),
			HeapUsagePercent: heapUsage,
		},
		Disk: disk,
		Network: sohaapi.RuntimeNetworkUsage{
			Available:        networkAvailable,
			Scope:            networkScope(networkAvailable),
			RxBytes:          rxBytes,
			TxBytes:          txBytes,
			RxBytesPerSecond: rxRate,
			TxBytesPerSecond: txRate,
		},
		GoRuntime: sohaapi.RuntimeGoUsage{
			Goroutines: runtime.NumGoroutine(),
			GcCycles:   int64(memory.NumGC),
			Gomaxprocs: runtime.GOMAXPROCS(0),
		},
		Services: aggregateServices(c.metrics),
	}
}

func (c *Collector) rates(now time.Time, cpuTime time.Duration, rxBytes, txBytes int64, networkAvailable bool) (float64, float64, float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	previous := c.last
	c.last = sample{at: now, cpuTime: cpuTime, networkRX: rxBytes, networkTX: txBytes}
	if previous.at.IsZero() {
		wall := now.Sub(c.started).Seconds()
		return normalizedCPUPercent(cpuTime.Seconds(), wall), 0, 0
	}

	wall := now.Sub(previous.at).Seconds()
	if wall <= 0 {
		return 0, 0, 0
	}
	cpuPercent := normalizedCPUPercent((cpuTime - previous.cpuTime).Seconds(), wall)
	if !networkAvailable {
		return cpuPercent, 0, 0
	}
	return cpuPercent, byteRate(rxBytes-previous.networkRX, wall), byteRate(txBytes-previous.networkTX, wall)
}

func normalizedCPUPercent(cpuSeconds, wallSeconds float64) float64 {
	if wallSeconds <= 0 || cpuSeconds <= 0 {
		return 0
	}
	return clamp(cpuSeconds/wallSeconds/float64(max(1, runtime.NumCPU()))*100, 0, 100)
}

func byteRate(delta int64, seconds float64) float64 {
	if delta <= 0 || seconds <= 0 {
		return 0
	}
	return float64(delta) / seconds
}

func networkScope(available bool) sohaapi.RuntimeNetworkUsageScope {
	if available {
		return sohaapi.NetworkNamespace
	}
	return sohaapi.Unavailable
}

func aggregateServices(metrics MetricsProvider) sohaapi.RuntimeServiceUsage {
	if metrics == nil {
		return sohaapi.RuntimeServiceUsage{}
	}
	snapshot := metrics.Snapshot()
	components := []runtimeobs.ComponentSnapshot{
		snapshot.ClusterSync,
		snapshot.WorkflowRunner,
		snapshot.CopilotInspection,
		snapshot.VirtualizationWorker,
		snapshot.DirectorySync,
	}
	result := sohaapi.RuntimeServiceUsage{}
	for _, component := range components {
		result.Started += component.Started
		result.Succeeded += component.Succeeded
		result.Failed += component.Failed
		result.Canceled += component.Canceled
		result.QueueDepth += component.QueueDepth
	}
	return result
}

func percent(value, total int64) float64 {
	if value <= 0 || total <= 0 {
		return 0
	}
	return clamp(float64(value)/float64(total)*100, 0, 100)
}

func clamp(value, low, high float64) float64 {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
