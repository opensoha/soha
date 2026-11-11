import { useState } from 'react'
import { Alert, Button, Card, Input, Select, Space, Typography } from 'antd'
import { useMutation } from '@tanstack/react-query'
import type { RepositoryAnalysisInput } from '@opensoha/contracts/gen/ts/sohaapi'
import { deliveryApi } from '../api'
import type { DeliveryRepository } from '../types'

const statusLabels = {
  identified: '已识别项目',
  multiple_candidates: '发现多个候选，请选择项目目录',
  unrecognized: '未识别到支持的项目',
  unsupported: '当前仓库尚不支持分析',
  forbidden: '没有代码源读取权限',
  read_failed: '源码读取失败',
}

export function RepositoryAnalysis({
  applicationId,
  repositories,
}: {
  applicationId: string
  repositories: DeliveryRepository[]
}) {
  const [repositoryId, setRepositoryId] = useState('')
  const [refType, setRefType] = useState<RepositoryAnalysisInput['refType']>('branch')
  const [refName, setRefName] = useState('')
  const [projectPath, setProjectPath] = useState('.')
  const selected = repositories.find((item) => item.id === repositoryId) ?? repositories[0]
  const input: RepositoryAnalysisInput = {
    repositoryId: selected?.id ?? '',
    refType,
    refName: refName || selected?.defaultBranch || 'main',
    projectPath,
  }
  const analysis = useMutation({
    mutationFn: (request: RepositoryAnalysisInput) =>
      deliveryApi.applications.analyzeRepository(applicationId, request),
  })
  const current = JSON.stringify(analysis.variables) === JSON.stringify(input)
  const result = current ? analysis.data : undefined
  return (
    <Card size="small" title="源码识别">
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Space wrap>
          <Select
            aria-label="分析仓库"
            style={{ minWidth: 180 }}
            value={selected?.id}
            options={repositories.map((item) => ({ value: item.id, label: item.name }))}
            onChange={(value) => {
              setRepositoryId(value)
              setRefName('')
            }}
          />
          <Select
            aria-label="分析版本类型"
            value={refType}
            onChange={setRefType}
            options={[
              { value: 'branch', label: '分支' },
              { value: 'tag', label: 'Tag' },
              { value: 'commit', label: 'Commit' },
            ]}
          />
          <Input
            aria-label="分析版本"
            value={refName}
            placeholder={selected?.defaultBranch || 'main'}
            onChange={(event) => setRefName(event.target.value)}
          />
          <Input
            aria-label="项目目录"
            value={projectPath}
            placeholder="仓库内的项目目录，如 services/api"
            onChange={(event) => setProjectPath(event.target.value)}
          />
          <Button
            disabled={!selected}
            loading={analysis.isPending}
            onClick={() => analysis.mutate(input)}
          >
            分析源码
          </Button>
        </Space>
        <Typography.Text type="secondary">
          选择仓库内的项目目录，查看构建建议。分析结果不会修改已选构建方式。
        </Typography.Text>
        {current && analysis.error ? (
          <Alert type="error" showIcon title={analysis.error.message} />
        ) : null}
        {result ? (
          <>
            <Typography.Text strong>{statusLabels[result.status]}</Typography.Text>
            {result.resolvedCommit ? (
              <Typography.Text copyable code>
                {result.resolvedCommit}
              </Typography.Text>
            ) : null}
            {result.candidates.map((candidate, index) => (
              <div key={`${candidate.projectPath}:${candidate.language}:${index}`}>
                <Typography.Text>
                  {[
                    candidate.projectPath,
                    candidate.language,
                    candidate.packageManager,
                    candidate.versionRange,
                    candidate.framework,
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </Typography.Text>
                <div>
                  候选构建方式：
                  {candidate.buildMethods
                    .map((method) =>
                      method === 'repo_dockerfile'
                        ? '仓库 Dockerfile'
                        : 'Buildpacks（需兼容构建器）',
                    )
                    .join('、')}
                </div>
                <Typography.Text type="secondary">
                  依据：{candidate.evidencePaths.join('、')}
                </Typography.Text>
              </div>
            ))}
            {result.warnings.map((warning, index) => (
              <Alert
                key={`${warning.code}:${index}`}
                type="warning"
                showIcon
                title={warning.message}
                description={warning.path}
              />
            ))}
          </>
        ) : null}
      </Space>
    </Card>
  )
}
