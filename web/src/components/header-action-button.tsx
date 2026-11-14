import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from 'react'

interface HeaderActionButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  ariaLabel: string
  icon?: ReactNode
  iconClassName?: string
  inset?: boolean
  label?: ReactNode
  pressed?: boolean
}

export const HeaderActionButton = forwardRef<HTMLButtonElement, HeaderActionButtonProps>(function HeaderActionButton({
  ariaLabel,
  className,
  icon,
  iconClassName,
  inset = false,
  label,
  pressed,
  type = 'button',
  ...buttonProps
}, ref) {
  const hasLabel = label != null && label !== ''
  const buttonClassName = [
    'soha-header-action-button',
    hasLabel ? 'is-wide' : 'is-icon',
    inset ? 'has-inset' : '',
    pressed ? 'is-active' : '',
    className,
  ].filter(Boolean).join(' ')
  const iconClassNames = ['soha-header-action-button__icon', iconClassName].filter(Boolean).join(' ')
  const content = (
    <>
      {icon ? <span className={iconClassNames}>{icon}</span> : null}
      {hasLabel ? <span className="soha-header-action-button__label">{label}</span> : null}
    </>
  )

  return (
    <button
      {...buttonProps}
      ref={ref}
      type={type}
      className={buttonClassName}
      aria-label={ariaLabel}
      aria-pressed={pressed}
    >
      {inset ? (
        <span className="soha-header-action-button__surface">
          {content}
        </span>
      ) : content}
    </button>
  )
})
