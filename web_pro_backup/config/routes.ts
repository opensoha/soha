/**
 * @name 简单版路由配置
 * @description 此配置用于 npm run simple 命令执行后使用
 */
export default [
  {
    path: '/login',
    layout: false,
    component: './login',
  },
  {
    path: '/',
    name: '概览',
    icon: 'dashboard',
    access: 'route:overview',
    component: './overview',
  },
  {
    path: '/clusters',
    name: '集群管理',
    icon: 'cluster',
    access: 'route:clusters',
    component: './platform/clusters',
  },
  {
    path: '/workloads/overview',
    name: '工作负载概览',
    icon: 'appstore',
    access: 'route:workloads-overview',
    component: './platform/workloads',
  },
  {
    path: '/workloads',
    redirect: '/workloads/overview',
  },
  {
    component: './exception/404',
    layout: false,
    path: '*',
  },
];
