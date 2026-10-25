import React from 'react';

import styles from './examples.module.css';
import classnames from 'classnames';
import Layout from '@theme/Layout';
import Link from '@docusaurus/Link';

import useDocusaurusContext from '@docusaurus/useDocusaurusContext';
import useBaseUrl from '@docusaurus/useBaseUrl';

function Examples() {
  const context = useDocusaurusContext();
  const headerImg = useBaseUrl('/img/go-templates.webp');

  return (
    <Layout
      title="Examples"
      description="doger examples and upstream samber/do project templates"
    >
      <header className="hero">
        <div className="container text--center">
          <h1>Examples and templates</h1>
          <div className="hero--subtitle">
            Here are doger's examples, and a few project templates from upstream
            `samber/do`, which doger is forked from.
          </div>
          <img
            className={styles.headerImg}
            src={headerImg}
            width={800}
            height={800}
            alt="Gopher mascot holding project templates for samber/do"
          />
        </div>
      </header>
      <main>
        <div className="container">
          <div className="row margin-vert--lg">
            <div className="col text--center padding-vert--md">
              <div className="card">
                <div className="card__header">
                  <i className={classnames(styles.icon)}>🚀</i>
                </div>
                <div className="card__body">
                  <p>doger &gt; examples</p>
                </div>
                <div className="card__footer">
                  <Link
                    to="https://github.com/kartaladev/doger/tree/main/examples"
                    className="button button--outline button--primary button--block"
                  >
                    See examples
                  </Link>
                </div>
              </div>
            </div>

            <div className="col text--center padding-vert--md">
              <div className="card">
                <div className="card__header">
                  <i className={classnames(styles.icon)}>🚀</i>
                </div>
                <div className="card__body">
                  <p>samber/do-template-api</p>
                </div>
                <div className="card__footer">
                  <Link
                    to="https://github.com/samber/do-template-api"
                    className="button button--outline button--primary button--block"
                  >
                    Clone
                  </Link>
                </div>
              </div>
            </div>

            <div className="col text--center padding-vert--md">
              <div className="card">
                <div className="card__header">
                  <i className={classnames(styles.icon)}>🚀</i>
                </div>
                <div className="card__body">
                  <p>samber/do-template-worker</p>
                </div>
                <div className="card__footer">
                  <Link
                    to="https://github.com/samber/do-template-api"
                    className="button button--outline button--primary button--block"
                  >
                    Clone
                  </Link>
                </div>
              </div>
            </div>

            <div className="col text--center padding-vert--md">
              <div className="card">
                <div className="card__header">
                  <i className={classnames(styles.icon)}>🚀</i>
                </div>
                <div className="card__body">
                  <p>samber/do-template-cli</p>
                </div>
                <div className="card__footer">
                  <Link
                    to="https://github.com/samber/do-template-cli"
                    className="button button--outline button--primary button--block"
                  >
                    Clone
                  </Link>
                </div>
              </div>
            </div>
          </div>
        </div>
      </main>
    </Layout>
  );
}

export default Examples;
