/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React from 'react';
import { Card, Typography } from '@douyinfe/semi-ui';
import { cacheStatusBackground, formatTokens } from './utils';

const TokenMessageChart = ({ units, cacheBoundary, t }) => {
  if (!units?.length) return null;
  const maxTokens = Math.max(
    ...units.map((unit) => unit.estimated_tokens || 0),
    1,
  );

  return (
    <Card
      bodyStyle={{ padding: 12 }}
      style={{ borderRadius: 8 }}
      title={
        <div
          style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}
        >
          <Typography.Text strong>
            {t('Tokens per prompt field')}
          </Typography.Text>
          <Typography.Text type='tertiary' size='small'>
            {t('Field attribution is inferred')}
          </Typography.Text>
        </div>
      }
    >
      <div style={{ display: 'flex', alignItems: 'end', gap: 6, height: 116 }}>
        {units.map((unit) => {
          const height = Math.max(
            6,
            Math.round(((unit.estimated_tokens || 0) / maxTokens) * 96),
          );
          const isBreakpoint = cacheBoundary?.break_unit_index === unit.index;
          const tooltip = [
            `${t('Path')}: ${unit.path}`,
            `${t('Role')}: ${unit.role || t('Unknown')}`,
            `${t('Estimated tokens')}: ${formatTokens(unit.estimated_tokens)}`,
            `${t('Cumulative range')}: ${formatTokens(unit.cumulative_start)} - ${formatTokens(unit.cumulative_end)}`,
            `${t('Cache status')}: ${unit.cache_status}`,
            `${t('Cache overlap')}: ${formatTokens(unit.cache_overlap_tokens)}`,
            `${t('Confidence')}: ${unit.confidence}`,
          ].join('\n');
          return (
            <div
              key={`${unit.index}-${unit.path}`}
              title={tooltip}
              style={{
                flex: '1 1 18px',
                minWidth: 12,
                maxWidth: 42,
                height,
                borderRadius: '4px 4px 0 0',
                background:
                  unit.cache_status === 'partial'
                    ? `repeating-linear-gradient(45deg, ${cacheStatusBackground(unit.cache_status)}, ${cacheStatusBackground(unit.cache_status)} 4px, transparent 4px, transparent 8px)`
                    : cacheStatusBackground(unit.cache_status),
                opacity: unit.cache_status === 'miss' ? 0.55 : 0.95,
                borderLeft: isBreakpoint
                  ? '2px solid var(--semi-color-danger)'
                  : undefined,
              }}
            />
          );
        })}
      </div>
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginTop: 8 }}>
        {['hit', 'partial', 'miss', 'unknown'].map((status) => (
          <Typography.Text key={status} type='tertiary' size='small'>
            <span
              style={{
                display: 'inline-block',
                width: 8,
                height: 8,
                borderRadius: 999,
                marginRight: 4,
                background: cacheStatusBackground(status),
              }}
            />
            {status}
          </Typography.Text>
        ))}
        {cacheBoundary?.break_unit_path && (
          <Typography.Text type='tertiary' size='small'>
            {t('Breakpoint')}: {cacheBoundary.break_unit_path}
          </Typography.Text>
        )}
      </div>
    </Card>
  );
};

export default TokenMessageChart;
