export interface DashboardTemplate {
  available: boolean
  description: string
  key: string
  name: string
  reason?: string
  json?: string
}

function template(
  key: string,
  name: string,
  description: string,
  panels: Array<{ title: string; type?: string; expr: string }>,
): DashboardTemplate {
  return {
    available: true,
    description,
    key,
    name,
    json: JSON.stringify({
      uid: `soha-${key}`,
      title: name,
      tags: ['soha-template', key],
      schemaVersion: 39,
      panels: panels.map((panel, index) => ({
        id: index + 1,
        title: panel.title,
        type: panel.type ?? 'timeseries',
        gridPos: { x: (index % 2) * 12, y: Math.floor(index / 2) * 8, w: 12, h: 8 },
        datasource: { type: 'prometheus', uid: 'soha-selected-prometheus' },
        targets: [{ refId: 'A', expr: panel.expr }],
      })),
    }),
  }
}

export const dashboardTemplates: DashboardTemplate[] = [
  template('kubernetes-workloads', 'Kubernetes 工作负载', 'Pod CPU、内存与重启趋势', [
    {
      title: 'Pod CPU',
      expr: 'sum by (namespace, pod) (rate(container_cpu_usage_seconds_total{container!=""}[$__rate_interval]))',
    },
    {
      title: 'Pod 内存',
      expr: 'sum by (namespace, pod) (container_memory_working_set_bytes{container!=""})',
    },
    {
      title: '容器重启',
      type: 'table',
      expr: 'sum by (namespace, pod) (increase(kube_pod_container_status_restarts_total[$__range]))',
    },
  ]),
  template('kubevirt-vmi', 'KubeVirt VMI', '虚拟机 CPU、内存与网络吞吐', [
    {
      title: 'VMI CPU',
      expr: 'sum by (namespace, name) (rate(kubevirt_vmi_cpu_usage_seconds_total[$__rate_interval]))',
    },
    {
      title: 'VMI 内存',
      expr: 'max by (namespace, name) (kubevirt_vmi_memory_resident_bytes)',
    },
    {
      title: 'VMI 网络接收',
      expr: 'sum by (namespace, name) (rate(kubevirt_vmi_network_receive_bytes_total[$__rate_interval]))',
    },
    {
      title: 'VMI 网络发送',
      expr: 'sum by (namespace, name) (rate(kubevirt_vmi_network_transmit_bytes_total[$__rate_interval]))',
    },
  ]),
  template('service-red', '服务 RED', '请求速率、错误率与 P95 延迟', [
    {
      title: '请求速率',
      expr: 'sum by (service_name) (rate(http_server_request_duration_seconds_count[$__rate_interval]))',
    },
    {
      title: '错误率',
      type: 'stat',
      expr: 'sum by (service_name) (rate(http_server_request_duration_seconds_count{http_response_status_code=~"5.."}[$__rate_interval])) / sum by (service_name) (rate(http_server_request_duration_seconds_count[$__rate_interval]))',
    },
    {
      title: 'P95 延迟',
      expr: 'histogram_quantile(0.95, sum by (le, service_name) (rate(http_server_request_duration_seconds_bucket[$__rate_interval])))',
    },
  ]),
  {
    available: false,
    description: '等待 PVE Prometheus 指标适配器',
    key: 'pve-resources',
    name: 'PVE 资源',
    reason: '当前 PVE 指标由 Provider API 查询，尚未接入 Dashboard 的 Prometheus 执行面。',
  },
]

export function dashboardTemplateJSON(key: string) {
  return dashboardTemplates.find((item) => item.key === key && item.available)?.json
}
