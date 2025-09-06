import type { ReactNode } from 'react';
import clsx from 'clsx';
import Heading from '@theme/Heading';
import styles from './styles.module.css';

type FeatureItem = {
  title: string;
  Svg: React.ComponentType<React.ComponentProps<'svg'>>;
  description: ReactNode;
};

const FeatureList: FeatureItem[] = [
  {
    title: 'WebDAV Interface',
    Svg: require('@site/static/img/webdav_featured.svg').default,
    description: (
      <>
        Mount Usenet content as a standard filesystem via WebDAV. Stream media files
        directly without waiting for complete downloads. Compatible with all major WebDAV clients.
      </>
    ),
  },
  {
    title: 'ARR Integration',
    Svg: require('@site/static/img/arr_integration.svg').default,
    description: (
      <>
        Seamless integration with Radarr, Sonarr, and other ARR applications. SABnzbd-compatible
        API for easy migration from existing setups. Automatic health monitoring and repair.
      </>
    ),
  },
  {
    title: 'High Performance',
    Svg: require('@site/static/img/high_performance.svg').default,
    description: (
      <>
        Optimized for speed with concurrent downloads, intelligent caching, and range request support.
        Multi-provider support with automatic failover for maximum reliability and performance.
      </>
    ),
  },
];

function Feature({ title, Svg, description }: FeatureItem) {
  return (
    <div className={clsx('col col--4')}>
      <div className="text--center">
        <Svg className={styles.featureSvg} role="img" fill='currentColor' />
      </div>
      <div className="text--center padding-horiz--md">
        <Heading as="h3">{title}</Heading>
        <p>{description}</p>
      </div>
    </div>
  );
}

export default function HomepageFeatures(): ReactNode {
  return (
    <section className={styles.features}>
      <div className="container">
        <div className="row">
          {FeatureList.map((props, idx) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: no need lint
            <Feature key={idx} {...props} />
          ))}
        </div>
      </div>
    </section>
  );
}
