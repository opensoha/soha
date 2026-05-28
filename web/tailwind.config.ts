import type { Config } from 'tailwindcss'

const config: Config = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        primary: 'var(--soha-primary)',
        'primary-hover': 'var(--soha-primary-hover)',
        'primary-active': 'var(--soha-primary-active)',
        'primary-light-default': 'var(--soha-primary-light-default)',
        success: 'var(--ant-colorSuccess)',
        warning: 'var(--ant-colorWarning)',
        danger: 'var(--soha-danger)',
        info: 'var(--ant-colorInfo)',
        'bg-0': 'var(--soha-bg-layout)',
        'bg-1': 'var(--soha-bg-surface)',
        'bg-2': 'var(--soha-bg-surface-muted)',
        'bg-3': 'var(--soha-bg-surface-elevated)',
        'text-0': 'var(--soha-text-primary)',
        'text-1': 'var(--soha-text-secondary)',
        'text-2': 'var(--soha-text-tertiary)',
        'text-3': 'var(--soha-text-quaternary)',
        border: 'var(--soha-border-color)',
        fill: 'var(--soha-fill-weak)',
      },
    },
  },
  corePlugins: {
    preflight: false,
  },
  plugins: [],
}

export default config
