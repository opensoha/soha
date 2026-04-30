import type { Config } from 'tailwindcss'

const config: Config = {
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        primary: 'var(--semi-color-primary)',
        'primary-hover': 'var(--semi-color-primary-hover)',
        'primary-active': 'var(--semi-color-primary-active)',
        'primary-light-default': 'var(--semi-color-primary-light-default)',
        success: 'var(--semi-color-success)',
        warning: 'var(--semi-color-warning)',
        danger: 'var(--semi-color-danger)',
        info: 'var(--semi-color-info)',
        'bg-0': 'var(--semi-color-bg-0)',
        'bg-1': 'var(--semi-color-bg-1)',
        'bg-2': 'var(--semi-color-bg-2)',
        'bg-3': 'var(--semi-color-bg-3)',
        'text-0': 'var(--semi-color-text-0)',
        'text-1': 'var(--semi-color-text-1)',
        'text-2': 'var(--semi-color-text-2)',
        'text-3': 'var(--semi-color-text-3)',
        border: 'var(--semi-color-border)',
        fill: 'var(--semi-color-fill-0)',
      },
    },
  },
  corePlugins: {
    preflight: false,
  },
  plugins: [],
}

export default config
