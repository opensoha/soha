import type { CSSProperties } from 'react'
import { AppstoreOutlined, LogoutOutlined, SettingOutlined, UserOutlined } from '@ant-design/icons'
import { Avatar, Button, Dropdown } from 'antd'
import { useNavigate } from 'react-router-dom'
import { logoutAuthSession } from '@/features/auth'
import { useI18n } from '@/i18n'
import { useAuthStore } from '@/stores/auth-store'

export function PortalUserAvatar({ size }: { size: number }) {
  const user = useAuthStore((state) => state.user)
  const avatarFit =
    user?.avatarFit === 'contain' || user?.avatarFit === 'fill' ? user.avatarFit : 'cover'

  return (
    <Avatar
      className="soha-portal-user-avatar"
      icon={<UserOutlined />}
      size={size}
      src={user?.avatarUrl || undefined}
      style={{ '--soha-avatar-fit': avatarFit } as CSSProperties}
    />
  )
}

export function PortalAccountMenu() {
  const navigate = useNavigate()
  const { t } = useI18n()
  const user = useAuthStore((state) => state.user)
  const userDisplayName = user?.userName ?? user?.email ?? t('layout.user', 'User')

  return (
    <Dropdown
      menu={{
        items: [
          { key: 'user', label: userDisplayName, disabled: true },
          { type: 'divider' },
          {
            key: 'workspace',
            icon: <AppstoreOutlined />,
            label: t('layout.workspace', 'Workspace'),
          },
          {
            key: 'accountSettings',
            icon: <SettingOutlined />,
            label: t('layout.accountSettings', 'Personal Settings'),
          },
          { type: 'divider' },
          {
            key: 'logout',
            icon: <LogoutOutlined />,
            label: t('layout.logout', 'Sign out'),
          },
        ],
        onClick: ({ key }) => {
          if (key === 'workspace') {
            navigate('/')
            return
          }
          if (key === 'accountSettings') {
            navigate('/account/settings')
            return
          }
          if (key === 'logout') {
            void logoutAuthSession().finally(() => navigate('/login'))
          }
        },
      }}
      placement="bottomRight"
      trigger={['click']}
    >
      <Button
        aria-label={t('layout.accountMenu', 'Account menu')}
        className="soha-portal-account-trigger"
        icon={<PortalUserAvatar size={24} />}
      >
        {userDisplayName}
      </Button>
    </Dropdown>
  )
}
