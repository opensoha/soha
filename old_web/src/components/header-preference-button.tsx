import type { ReactNode } from 'react'

interface HeaderPreferenceButtonProps {
  ariaLabel: string
  icon: ReactNode
  inset?: boolean
  label?: string
  onClick: () => void
  pressed?: boolean
  title?: string
}

export function HeaderPreferenceButton({
  ariaLabel,
  icon,
  inset = false,
  label,
  onClick,
  pressed,
  title,
}: HeaderPreferenceButtonProps) {
  const content = (
    <>
      <span className="kc-header-preference-button__icon">{icon}</span>
      {label ? <span className="kc-header-preference-button__label">{label}</span> : null}
    </>
  )

  return (
    <button
      type="button"
      className={`kc-header-preference-button ${label ? 'is-wide' : 'is-icon'} ${inset ? 'has-inset' : ''} ${pressed ? 'is-active' : ''}`.trim()}
      onClick={onClick}
      aria-label={ariaLabel}
      aria-pressed={pressed}
      title={title}
    >
      {inset ? (
        <span className="kc-header-preference-button__surface">
          {content}
        </span>
      ) : content}
    </button>
  )
}
