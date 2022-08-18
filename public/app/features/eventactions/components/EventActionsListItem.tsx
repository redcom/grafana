import { css, cx } from '@emotion/css';
import React, { memo } from 'react';

import { GrafanaTheme2 } from '@grafana/data';
import { HorizontalGroup, IconButton, useStyles2 } from '@grafana/ui';
import { contextSrv } from 'app/core/core';
import { AccessControlAction, EventActionsDTO } from 'app/types';

type EventActionListItemProps = {
  eventAction: EventActionsDTO;
  onRemoveButtonClick: (eventAction: EventActionsDTO) => void;
  onExecuteButtonClick: (eventAction: EventActionsDTO) => void;
};

const getEventActionsAriaLabel = (name: string) => {
  return `Edit event action's ${name} details`;
};

const EventActionListItem = memo(
  ({
    eventAction,
    onRemoveButtonClick,
    onExecuteButtonClick,
  }: EventActionListItemProps) => {
    const editUrl = `org/eventactions/${eventAction.id}`;
    const styles = useStyles2(getStyles);

    const linkedColumn = (content: string) =>
      <a
        className="ellipsis"
        href={editUrl}
        title={eventAction.name}
        aria-label={getEventActionsAriaLabel(eventAction.name)}
      >
        {content}
      </a>

    return (
      <tr key={eventAction.id} className={cx({})}>
        <td className="link-td max-width-10">
          {linkedColumn(eventAction.name)}
        </td>
        <td>
          {linkedColumn(eventAction.type)}
        </td>
        <td>
          {linkedColumn(eventAction.url)}
        </td>
        <td>
          <HorizontalGroup justify="flex-end">
            {contextSrv.hasPermission(AccessControlAction.EventActionsWrite) && (
              <IconButton
                className={styles.deleteButton}
                name="rocket"
                size="md"
                onClick={() => onExecuteButtonClick(eventAction)}
                aria-label={`Execute event action ${eventAction.name}`}
              />
            )}
            {contextSrv.hasPermission(AccessControlAction.EventActionsDelete) && (
              <IconButton
                className={styles.deleteButton}
                name="trash-alt"
                size="md"
                onClick={() => onRemoveButtonClick(eventAction)}
                aria-label={`Delete event action ${eventAction.name}`}
              />
            )}
          </HorizontalGroup>
        </td>
      </tr>
    );
  }
);
EventActionListItem.displayName = 'EventActionListItem';

const getStyles = (theme: GrafanaTheme2) => {
  return {
    iconRow: css`
      svg {
        margin-left: ${theme.spacing(0.5)};
      }
    `,
    accountId: cx(
      'ellipsis',
      css`
        color: ${theme.colors.text.secondary};
      `
    ),
    deleteButton: css`
      color: ${theme.colors.text.secondary};
    `,
    disabled: css`
      td a {
        color: ${theme.colors.text.secondary};
      }
    `,
  };
};

export default EventActionListItem;