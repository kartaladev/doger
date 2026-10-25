import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'doger - Go Dependency Injection',
  tagline: 'Type-safe DI for Go using generics',
  favicon: 'img/favicon.ico',

  // Future flags, see https://docusaurus.io/docs/api/docusaurus-config#future
  future: {
    v4: {
      removeLegacyPostBuildHeadAttribute: true,
      useCssCascadeLayers: true,
    },
    faster: {
      swcJsLoader: true,
      swcJsMinimizer: true,
      swcHtmlMinimizer: true,
      lightningCssMinimizer: true,
      rspackBundler: true,
      rspackPersistentCache: true,
      ssgWorkerThreads: true,
      mdxCrossCompilerCache: true,
    },
  },
  storage: {
    type: 'localStorage',
    namespace: true,
  },

  // Set the production url of your site here
  url: 'https://kartaladev.github.io',
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: '/doger/',

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: 'kartaladev', // Usually your GitHub org/user name.
  projectName: 'doger', // Usually your repo name.

  // Optional: deployment branch
  // deploymentBranch: 'gh-pages',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'throw',
  onBrokenAnchors: 'throw',

  markdown: {
    anchors: {
      maintainCase: true,
    },
    mermaid: true,
  },

  // Storage configuration for better performance
  staticDirectories: ['static'],

  // Optional: Enable hash router for offline support (experimental)
  // Uncomment if you need offline browsing capability
  // router: 'hash',

  // Future-proofing configurations
  clientModules: [require.resolve('./src/theme/prism-include-languages.js')],

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  headTags: [
    // SEO
    {
      tagName: 'meta',
      attributes: {
        property: 'og:image',
        content: 'https://kartaladev.github.io/doger/img/cover.png',
      },
    },
    {
      tagName: 'meta',
      attributes: {
        name: 'twitter:card',
        content: 'summary_large_image',
      },
    },
    {
      tagName: 'meta',
      attributes: {
        name: 'twitter:image',
        content: 'https://kartaladev.github.io/doger/img/cover.png',
      },
    },
    // NOTE: do NOT add a static <meta property="og:locale"> here.
    // Docusaurus already emits one automatically from `i18n.defaultLocale`
    // (rendered as `en`); a second static tag here previously conflicted
    // with it (`en_US`), which is an invalid/duplicate signal for crawlers.
    // og:site_name provides branding context in social share cards
    {
      tagName: 'meta',
      attributes: {
        property: 'og:site_name',
        content: 'doger',
      },
    },
    // NOTE: do NOT add a global <link rel="canonical"> here.
    // Docusaurus injects a correct per-page canonical automatically
    // based on `url` + `baseUrl` + the page path. A static href here
    // would override every page's canonical to the homepage, causing
    // Google to treat all docs pages as non-canonical duplicates.
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          sidebarPath: './sidebars.ts',
          // Please change this to your repo.
          // Remove this to remove the "edit this page" links.
          editUrl: 'https://github.com/kartaladev/doger/tree/main/docs/',
          showLastUpdateAuthor: true,
          showLastUpdateTime: true,
          // Enhanced docs features from 3.8+
          breadcrumbs: true,
          sidebarCollapsed: false,
          numberPrefixParser: false,
          // Enable admonitions
          admonitions: {
            keywords: ['note', 'tip', 'info', 'danger', 'warning'],
            extendDefaults: true,
          },
          // Enhanced markdown features
          remarkPlugins: [],
          rehypePlugins: [],
        },
        // The blog feature is unused (no posts). Leaving it on the default
        // `blog: {}` creates an empty, indexable /blog page with thin content.
        blog: false,
        sitemap: {
          lastmod: 'date',
          changefreq: 'weekly',
          priority: 0.7,
          ignorePatterns: ['/tags/**', '/search'],
          filename: 'sitemap.xml',
          // Enhanced sitemap features from 3.8+
          createSitemapItems: async (params) => {
            const {defaultCreateSitemapItems, ...rest} = params;
            const items = await defaultCreateSitemapItems(rest);
            // Add custom priority for specific pages
            return items.map((item) => {
              if (
                item.url === 'https://kartaladev.github.io/doger/' ||
                item.url.includes('/docs/getting-started')
              ) {
                return {...item, priority: 1.0};
              }
              if (item.url.includes('/docs/')) {
                return {...item, priority: 0.8};
              }
              return item;
            });
          },
        },
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    // Replace with your project's social card
    image: 'img/cover.png',
    colorMode: {
      defaultMode: 'light',
      disableSwitch: false,
      respectPrefersColorScheme: true,
    },

    // Mermaid configuration
    mermaid: {
      theme: {light: 'neutral', dark: 'dark'},
      options: {
        maxTextSize: 50000,
      },
    },

    // Enhanced metadata
    // og:type defaults to "website"; individual doc pages that need
    // "article" should override via their page's <Layout> or frontmatter.
    metadata: [
      {name: 'og:type', content: 'website'},
      // Fallback description for pages that don't set their own
      {
        name: 'description',
        content:
          'Type-safe dependency injection for Go using generics. A drop-in replacement for uber/dig with a fluent API and zero reflection.',
      },
    ],

    navbar: {
      title: '⚙️ doger',
      logo: {
        alt: 'doger - Dependency Injection for Go',
        src: 'img/icon.png',
        width: 32,
        height: 32,
      },
      hideOnScroll: false,
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docSidebar',
          position: 'left',
          // label: 'Guides',
          label: 'Doc',
        },
        {
          to: 'examples',
          label: 'Examples',
          position: 'left',
        },
        {
          to: 'https://pkg.go.dev/github.com/kartaladev/doger',
          label: 'GoDoc',
          position: 'left',
        },
        {
          to: 'community',
          label: 'Community',
          position: 'left',
        },
        {
          type: 'dropdown',
          label: 'v0',
          position: 'right',
          items: [
            {label: 'v0', to: '/docs/about', disabled: true},
            {
              label: 'upstream samber/do v1',
              href: 'https://github.com/samber/do/tree/v1',
            },
          ],
        },
        {
          to: 'https://github.com/kartaladev/doger/releases',
          label: 'Changelog',
          position: 'right',
        },
        {
          href: 'https://github.com/kartaladev/doger',
          // label: 'GitHub',
          position: 'right',
          className: 'header-github-link',
          'aria-label': 'GitHub repository',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Project',
          items: [
            {
              label: 'Documentation',
              to: '/docs/getting-started',
            },
            {
              label: 'Changelog',
              to: 'https://github.com/kartaladev/doger/releases',
            },
            {
              label: 'Godoc',
              to: 'https://pkg.go.dev/github.com/kartaladev/doger',
            },
            {
              label: 'License',
              to: 'https://github.com/kartaladev/doger/blob/main/LICENSE',
            },
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'New issue',
              to: 'https://github.com/kartaladev/doger/issues',
            },
            {
              label: 'GitHub',
              to: 'https://github.com/kartaladev/doger',
            },
          ],
        },
        {
          title: 'Comparisons',
          items: [
            {
              label: 'vs uber/fx',
              to: '/docs/comparison/samber-do-vs-uber-fx',
            },
            {
              label: 'vs google/wire',
              to: '/docs/comparison/samber-do-vs-google-wire',
            },
            {
              label: 'vs uber/dig',
              to: '/docs/comparison/samber-do-vs-uber-dig',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} doger.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      defaultLanguage: 'go',
      additionalLanguages: ['bash', 'diff', 'json', 'yaml', 'go'],
      magicComments: [
        {
          className: 'theme-code-block-highlighted-line',
          line: 'highlight-next-line',
          block: {start: 'highlight-start', end: 'highlight-end'},
        },
        {
          className: 'code-block-error-line',
          line: 'error-next-line',
          block: {start: 'error-start', end: 'error-end'},
        },
      ],
    },
  } satisfies Preset.ThemeConfig,

  themes: ['@docusaurus/theme-mermaid'],

  plugins: [
    // Add ideal image plugin for better image optimization
    [
      '@docusaurus/plugin-ideal-image',
      {
        quality: 70,
        max: 1030,
        min: 640,
        steps: 2,
        disableInDev: false,
      },
    ],
    [
      'vercel-analytics',
      {
        debug: true,
        mode: 'auto',
      },
    ],
    require.resolve('./plugins/structured-data'),
    require.resolve('./plugins/chunk-splitting'),
  ],
};

export default config;
