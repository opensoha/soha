import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const docsSiteUrl = process.env.DOCS_SITE_URL ?? 'http://localhost';
const docsBaseUrl = process.env.DOCS_BASE_URL ?? '/docs/';
const showDocsLastUpdateTime = process.env.DOCS_SHOW_LAST_UPDATE_TIME !== 'false';

const config: Config = {
  title: 'soha Docs',
  tagline: 'Multi-cluster Kubernetes platform console documentation',
  favicon: 'logo.svg',
  future: {
    v4: true,
  },
  url: docsSiteUrl,
  baseUrl: docsBaseUrl,
  trailingSlash: true,
  onBrokenLinks: 'throw',
  markdown: {
    hooks: {
      onBrokenMarkdownLinks: 'throw',
    },
  },
  i18n: {
    defaultLocale: 'zh-Hans',
    locales: ['zh-Hans'],
  },
  staticDirectories: ['public'],
  presets: [
    [
      'classic',
      {
        docs: {
          path: '.',
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          include: ['**/*.{md,mdx}'],
          exclude: [
            'en/**',
            '.vitepress/**',
            'node_modules/**',
            'build/**',
            'src/**',
            'static/**',
          ],
          showLastUpdateTime: showDocsLastUpdateTime,
          editUrl: 'https://github.com/soha/soha/tree/main/docs/',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],
  themeConfig: {
    image: 'logo.svg',
    colorMode: {
      respectPrefersColorScheme: true,
    },
    navbar: {
      title: 'soha Docs',
      hideOnScroll: true,
      logo: {
        alt: 'soha logo',
        src: 'logo.svg',
      },
      items: [
        {
          type: 'doc',
          docId: 'index',
          label: '首页',
          position: 'left',
        },
        {
          type: 'doc',
          docId: 'architecture/index',
          label: '架构',
          position: 'left',
        },
        {
          type: 'doc',
          docId: 'development/local-development',
          label: '开发',
          position: 'left',
        },
        {
          type: 'doc',
          docId: 'api/overview',
          label: 'API',
          position: 'left',
        },
        {
          type: 'doc',
          docId: 'operations/configuration',
          label: '运维',
          position: 'left',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: '文档',
          items: [
            {label: '架构', to: '/architecture/'},
            {label: '开发', to: '/development/local-development'},
            {label: 'API', to: '/api/overview'},
            {label: '运维', to: '/operations/configuration'},
          ],
        },
      ],
      copyright: '© 2026 Soha 版权所有，由项目贡献者设计与开发。',
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
