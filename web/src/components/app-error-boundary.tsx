import type { ReactNode } from 'react'
import { Component } from 'react'
import { Button, Result } from 'antd'

interface AppErrorBoundaryProps {
  children: ReactNode
}

interface AppErrorBoundaryState {
  error: Error | null
}

export class AppErrorBoundary extends Component<AppErrorBoundaryProps, AppErrorBoundaryState> {
  state: AppErrorBoundaryState = {
    error: null,
  }

  static getDerivedStateFromError(error: Error): AppErrorBoundaryState {
    return { error }
  }

  componentDidCatch(error: Error) {
    console.error(
      JSON.stringify({
        timestamp: new Date().toISOString(),
        level: 'error',
        component: 'react',
        service: 'soha-web',
        event: 'ui.render.failed',
        message: 'Unhandled React render error',
        error_type: error.name || 'Error',
      }),
    )
  }

  handleReload = () => {
    window.location.reload()
  }

  handleGoHome = () => {
    window.location.assign('/')
  }

  render() {
    if (!this.state.error) {
      return this.props.children
    }

    return (
      <main className="soha-error-boundary" role="alert">
        <Result
          status="500"
          title="页面渲染失败"
          subTitle="当前页面发生未处理错误，请重新加载。"
          extra={[
            <Button key="reload" type="primary" onClick={this.handleReload}>
              重新加载
            </Button>,
            <Button key="home" onClick={this.handleGoHome}>
              返回首页
            </Button>,
          ]}
        />
      </main>
    )
  }
}
