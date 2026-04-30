import { DefaultFooter } from '@ant-design/pro-components';
import React from 'react';

// Git commit hash, resolved at build time from env vars or git
const COMMIT_HASH = process.env.COMMIT_HASH || '';

const Footer: React.FC = () => {
  return (
    <DefaultFooter
      copyright={false}
      style={{
        background: 'none',
      }}
      links={[
        {
          key: 'version',
          title: `v${__APP_VERSION__}`,
          href: '#',
        },
        {
          key: 'umi',
          title: `Umi ${__UMI_VERSION__}`,
          href: 'https://umijs.org/',
          blankTarget: true,
        },
        {
          key: 'utoo',
          title: `Utoo ${__UTOO_VERSION__}`,
          href: 'https://utoo.land',
          blankTarget: true,
        },
        ...(COMMIT_HASH
          ? [
              {
                key: 'commit',
                title: COMMIT_HASH.slice(0, 7),
                href: '#',
              },
            ]
          : []),
        {
          key: 'product',
          title: 'KubeCrux Pro Console',
          href: '#',
        },
      ]}
    />
  );
};

export default Footer;
