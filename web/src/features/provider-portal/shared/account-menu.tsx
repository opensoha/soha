import type { CSSProperties } from 'react'
import { UserOutlined } from '@ant-design/icons'
import { Avatar } from 'antd'
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
