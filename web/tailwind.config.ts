import type { Config } from 'tailwindcss'

const config: Config = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        primary: 'var(--kc-primary)',
        'primary-hover': 'var(--kc-primary-hover)',
        'primary-active': 'var(--kc-primary-active)',
        'primary-light-default': 'var(--kc-primary-light-default)',
        success: 'var(--ant-colorSuccess)',
        warning: 'var(--ant-colorWarning)',
        danger: 'var(--kc-danger)',
        info: 'var(--ant-colorInfo)',
        'bg-0': 'var(--kc-bg-layout)',
        'bg-1': 'var(--kc-bg-surface)',
        'bg-2': 'var(--kc-bg-surface-muted)',
        'bg-3': 'var(--kc-bg-surface-elevated)',
        'text-0': 'var(--kc-text-primary)',
        'text-1': 'var(--kc-text-secondary)',
        'text-2': 'var(--kc-text-tertiary)',
        'text-3': 'var(--kc-text-quaternary)',
        border: 'var(--kc-border-color)',
        fill: 'var(--kc-fill-weak)',
      },
    },
  },
  corePlugins: {
    preflight: false,
  },
  plugins: [],
}

export default config
