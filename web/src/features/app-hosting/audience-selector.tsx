import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { Input, Radio, Select, Space, Typography } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { appHostingQueries } from './queries'
import {
  isAudienceNarrowed,
  type AppHostingAudienceTeam,
  type AppHostingAudienceUser,
  type HostedAppVisibilityInput,
  type HostedAppVisibilityMode,
  type HostedAppVisibilityScope,
} from './types'

const { Text } = Typography

export const AUDIENCE_NARROWING_WARNING =
  '保存后，被移除的同事搜不到作品，使用旧链接或刷新页面也会被拒绝'

export interface AudienceSelectorProps {
  value: HostedAppVisibilityInput
  onChange: (next: HostedAppVisibilityInput) => void
  reason?: string
  onReasonChange?: (reason: string) => void
  previousVisibility?: HostedAppVisibilityScope
  disabled?: boolean
  initialTeams?: AppHostingAudienceTeam[]
  initialUsers?: AppHostingAudienceUser[]
}

function formatTeamLabel(team: AppHostingAudienceTeam) {
  return team.slug ? `${team.name} (${team.slug})` : team.name
}

function formatUserLabel(user: AppHostingAudienceUser) {
  const identity = user.displayName || user.username || user.id
  return user.department ? `${identity} · ${user.department}` : identity
}

export function AudienceSelector({
  value,
  onChange,
  reason = '',
  onReasonChange,
  previousVisibility,
  disabled = false,
  initialTeams,
  initialUsers,
}: AudienceSelectorProps) {
  const [teamQuery, setTeamQuery] = useState('')
  const [userQuery, setUserQuery] = useState('')
  const teamLabelCache = useRef(new Map<string, string>())
  const userLabelCache = useRef(new Map<string, string>())

  const selectedEnabled = value.mode === 'selected' && !disabled
  const teamCandidatesQuery = useQuery(
    appHostingQueries.audienceCandidates({ query: teamQuery, limit: 25 }, selectedEnabled),
  )
  const userCandidatesQuery = useQuery(
    appHostingQueries.audienceCandidates({ query: userQuery, limit: 25 }, selectedEnabled),
  )

  const teamCandidates = readableQueryData(teamCandidatesQuery)?.teams ?? []
  const userCandidates = readableQueryData(userCandidatesQuery)?.users ?? []

  useEffect(() => {
    for (const team of initialTeams ?? []) {
      teamLabelCache.current.set(team.id, formatTeamLabel(team))
    }
    for (const team of teamCandidates) {
      teamLabelCache.current.set(team.id, formatTeamLabel(team))
    }
  }, [initialTeams, teamCandidates])

  useEffect(() => {
    for (const user of initialUsers ?? []) {
      userLabelCache.current.set(user.id, formatUserLabel(user))
    }
    for (const user of userCandidates) {
      userLabelCache.current.set(user.id, formatUserLabel(user))
    }
  }, [initialUsers, userCandidates])

  const selectedTeamIds = useMemo(() => value.teamIds ?? [], [value.teamIds])
  const selectedUserIds = useMemo(() => value.userIds ?? [], [value.userIds])

  const teamOptions = useMemo(() => {
    const merged = new Map<string, string>()
    for (const team of teamCandidates) {
      merged.set(team.id, formatTeamLabel(team))
    }
    for (const teamId of selectedTeamIds) {
      if (!merged.has(teamId)) {
        merged.set(teamId, teamLabelCache.current.get(teamId) ?? teamId)
      }
    }
    return Array.from(merged, ([id, label]) => ({ value: id, label }))
  }, [selectedTeamIds, teamCandidates])

  const userOptions = useMemo(() => {
    const merged = new Map<string, string>()
    for (const user of userCandidates) {
      merged.set(user.id, formatUserLabel(user))
    }
    for (const userId of selectedUserIds) {
      if (!merged.has(userId)) {
        merged.set(userId, userLabelCache.current.get(userId) ?? userId)
      }
    }
    return Array.from(merged, ([id, label]) => ({ value: id, label }))
  }, [selectedUserIds, userCandidates])

  const narrowed = isAudienceNarrowed(previousVisibility, value)

  const handleModeChange = (mode: HostedAppVisibilityMode) => {
    if (mode === 'private') {
      onChange({ mode: 'private' })
      return
    }
    if (mode === 'organization') {
      onChange({ mode: 'organization' })
      return
    }
    onChange({
      mode: 'selected',
      teamIds: selectedTeamIds,
      userIds: selectedUserIds,
    })
  }

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Radio.Group
        aria-label="可见范围模式"
        disabled={disabled}
        value={value.mode}
        onChange={(event) => handleModeChange(event.target.value as HostedAppVisibilityMode)}
      >
        <Space orientation="vertical" size={8}>
          <Radio value="private">
            <Text strong>仅自己（默认）</Text>
            <div>
              <Text type="secondary">
                仅作者本人可打开在线应用，「同事作品」目录隐藏该作品。
              </Text>
            </div>
          </Radio>
          <Radio value="selected">
            <Text strong>指定部门 / 个人</Text>
            <div>
              <Text type="secondary">
                支持跨部门选择任意团队或指定同事，获授权成员可在门户发现并打开。
              </Text>
            </div>
          </Radio>
          <Radio value="organization">
            <Text strong>全公司</Text>
            <div>
              <Text type="secondary">
                面向全体有效组织成员开放；若平台要求公开审批，等待期间继续保持当前范围生效。
              </Text>
            </div>
          </Radio>
        </Space>
      </Radio.Group>

      {value.mode === 'selected' ? (
        <Space orientation="vertical" size={10} style={{ width: '100%' }}>
          <ReadFeedback
            query={teamCandidatesQuery}
            label="受众部门候选"
            canRead={selectedEnabled}
          />
          <div>
            <Text>指定部门 / 团队（可跨部门多选）</Text>
            <Select
              allowClear
              aria-label="选择可见部门"
              disabled={disabled}
              loading={teamCandidatesQuery.isFetching}
              mode="multiple"
              options={teamOptions}
              placeholder="输入部门名称搜索并多选，例如：财务部、市场部"
              showSearch={{
                filterOption: false,
                onSearch: setTeamQuery,
              }}
              style={{ width: '100%', marginTop: 4 }}
              value={selectedTeamIds}
              onChange={(nextTeamIds: string[]) =>
                onChange({
                  mode: 'selected',
                  teamIds: nextTeamIds,
                  userIds: selectedUserIds,
                })
              }
            />
          </div>
          <div>
            <Text>指定同事（可跨部门多选）</Text>
            <Select
              allowClear
              aria-label="选择可见同事"
              disabled={disabled}
              loading={userCandidatesQuery.isFetching}
              mode="multiple"
              options={userOptions}
              placeholder="输入同事姓名或账号搜索并多选"
              showSearch={{
                filterOption: false,
                onSearch: setUserQuery,
              }}
              style={{ width: '100%', marginTop: 4 }}
              value={selectedUserIds}
              onChange={(nextUserIds: string[]) =>
                onChange({
                  mode: 'selected',
                  teamIds: selectedTeamIds,
                  userIds: nextUserIds,
                })
              }
            />
          </div>
          {selectedTeamIds.length === 0 && selectedUserIds.length === 0 ? (
            <Text type="warning">请至少选择一个部门或一位同事。</Text>
          ) : null}
        </Space>
      ) : null}

      {value.mode === 'organization' && onReasonChange ? (
        <div>
          <Text>全公司公开说明（可选）</Text>
          <Input.TextArea
            aria-label="全公司公开申请说明"
            disabled={disabled}
            maxLength={500}
            placeholder="可选填写用途或适用对象说明，便于管理员审批核对"
            rows={2}
            style={{ marginTop: 4 }}
            value={reason}
            onChange={(event) => onReasonChange(event.target.value)}
          />
        </div>
      ) : null}

      {narrowed ? (
        <ContextFeedback
          showIcon
          type="warning"
          title="即将收窄访问范围"
          description={AUDIENCE_NARROWING_WARNING}
        />
      ) : null}
    </Space>
  )
}
