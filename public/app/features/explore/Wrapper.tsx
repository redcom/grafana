import { css } from '@emotion/css';
import React, { useEffect } from 'react';

import { locationService } from '@grafana/runtime';
import { ErrorBoundaryAlert } from '@grafana/ui';
import { useGrafana } from 'app/core/context/GrafanaContext';
import { useNavModel } from 'app/core/hooks/useNavModel';
import { GrafanaRouteComponentProps } from 'app/core/navigation/types';
import { isTruthy } from 'app/core/utils/types';
import { useSelector, useDispatch } from 'app/types';
import { ExploreId, ExploreQueryParams } from 'app/types/explore';

import { Branding } from '../../core/components/Branding/Branding';

import { ExploreActions } from './ExploreActions';
import { ExplorePaneContainer } from './ExplorePaneContainer';
import { lastSavedUrl, resetExploreAction } from './state/main';

const styles = {
  pageScrollbarWrapper: css`
    width: 100%;
    flex-grow: 1;
    min-height: 0;
  `,
  exploreWrapper: css`
    display: flex;
    height: 100%;
  `,
};

function Wrapper(props: GrafanaRouteComponentProps<{}, ExploreQueryParams>) {
  useExplorePageTitle();
  const dispatch = useDispatch();
  const queryParams = props.queryParams;
  const { keybindings, chrome } = useGrafana();
  const navModel = useNavModel('explore');

  useEffect(() => {
    //This is needed for breadcrumbs and topnav.
    //We should probably abstract this out at some point
    chrome.update({ sectionNav: navModel.node });
  }, [chrome, navModel]);

  useEffect(() => {
    keybindings.setupTimeRangeBindings(false);
  }, [keybindings]);

  useEffect(() => {
    lastSavedUrl.left = undefined;
    lastSavedUrl.right = undefined;

    // timeSrv (which is used internally) on init reads `from` and `to` param from the URL and updates itself
    // using those value regardless of what is passed to the init method.
    // The updated value is then used by Explore to get the range for each pane.
    // This means that if `from` and `to` parameters are present in the URL,
    // it would be impossible to change the time range in Explore.
    // We are only doing this on mount for 2 reasons:
    // 1: Doing it on update means we'll enter a render loop.
    // 2: when parsing time in Explore (before feeding it to timeSrv) we make sure `from` is before `to` inside
    //    each pane state in order to not trigger un URL update from timeSrv.
    const searchParams = locationService.getSearchObject();
    if (searchParams.from || searchParams.to) {
      locationService.partial({ from: undefined, to: undefined }, true);
    }

    return () => {
      // Cleaning up Explore state so that when navigating back to Explore it starts from a blank state
      dispatch(resetExploreAction());
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- dispatch is stable, doesn't need to be in the deps array
  }, []);

  const hasSplit = Boolean(queryParams.left) && Boolean(queryParams.right);

  return (
    <div className={styles.pageScrollbarWrapper}>
      <ExploreActions exploreIdLeft={ExploreId.left} exploreIdRight={ExploreId.right} />
      <div className={styles.exploreWrapper}>
        <ErrorBoundaryAlert style="page">
          <ExplorePaneContainer split={hasSplit} exploreId={ExploreId.left} urlQuery={queryParams.left} />
        </ErrorBoundaryAlert>
        {hasSplit && (
          <ErrorBoundaryAlert style="page">
            <ExplorePaneContainer split={hasSplit} exploreId={ExploreId.right} urlQuery={queryParams.right} />
          </ErrorBoundaryAlert>
        )}
      </div>
    </div>
  );
}

const useExplorePageTitle = () => {
  const navModel = useNavModel('explore');
  const datasources = useSelector((state) =>
    [state.explore.left.datasourceInstance?.name, state.explore.right?.datasourceInstance?.name].filter(isTruthy)
  );

  const documentTitle = `${navModel.main.text} - ${datasources.join(' | ')} - ${Branding.AppTitle}`;
  document.title = documentTitle;
};

export default Wrapper;
