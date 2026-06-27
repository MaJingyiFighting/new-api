/*
Copyright (C) 2025-2026 QuantumNous

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

import React, { useCallback, useEffect, useState } from 'react';
import { Modal, Button, Progress, Typography, Spin, Tag, Space } from '@douyinfe/semi-ui';
import { API, showError } from '../../../../helpers';

const { Text } = Typography;

const clampPercent = (value) => {
  const v = Number(value);
  if (!Number.isFinite(v)) return 0;
  return Math.max(0, Math.min(100, v));
};

const getProgressColor = (usedPct) => {
  const p = clampPercent(usedPct);
  if (p >= 85) return 'red';
  if (p >= 60) return 'amber';
  return 'green';
};

const formatCountdown = (endUnix) => {
  const now = Math.floor(Date.now() / 1000);
  const remaining = Number(endUnix) - now;
  if (!Number.isFinite(remaining) || remaining <= 0) return '--';
  const hours = Math.floor(remaining / 3600);
  const minutes = Math.floor((remaining % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m`;
  return `${minutes}m`;
};

export const openCodingPlanQuotaModal = ({ t, record, onRefresh }) => {
  const modal = Modal.info({
    className: 'coding-plan-quota-modal',
    title: t('配额详情') || 'Quota Details',
    icon: null,
    width: 700,
    header: null,
    content: (
      <CodingPlanQuotaContent
        record={record}
        t={t}
        onRefresh={onRefresh}
        onClose={() => modal.destroy()}
      />
    ),
    footer: null,
    closable: true,
  });
  return modal;
};

const CodingPlanQuotaContent = ({ record, t, onRefresh, onClose }) => {
  const [quotaData, setQuotaData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);

  const fetchQuota = useCallback(async () => {
    if (!record?.id) return;
    try {
      const res = await API.get(`/api/channel/update_quota/${record.id}`);
      const { success, message, quota_info } = res.data;
      if (success && quota_info) {
        setQuotaData(quota_info);
      } else {
        showError(message || t('查询失败'));
      }
    } catch (err) {
      showError(err.message || t('查询失败'));
    } finally {
      setLoading(false);
    }
  }, [record?.id, t]);

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    await fetchQuota();
    setRefreshing(false);
    if (onRefresh) onRefresh();
  }, [fetchQuota, onRefresh]);

  useEffect(() => {
    fetchQuota();
  }, [fetchQuota]);

  const keys = record?.keys ? record.keys.split('\n').filter(Boolean) : [];
  const keyEntries = quotaData
    ? Object.entries(quotaData)
    : [];

  return (
    <div style={{ padding: '16px 0' }}>
      <div style={{ marginBottom: 16, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <Text strong style={{ fontSize: 18 }}>
          {record?.name || ''} — {t('配额详情')}
        </Text>
        <Button
          loading={refreshing}
          onClick={handleRefresh}
          size='small'
        >
          {t('刷新')}
        </Button>
      </div>

      {loading ? (
        <div style={{ textAlign: 'center', padding: 40 }}>
          <Spin />
        </div>
      ) : keyEntries.length === 0 ? (
        <Text type='tertiary'>{t('暂无配额数据')}</Text>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {keyEntries.map(([keyIdxStr, info]) => {
            const idx = Number(keyIdxStr);
            const usedPct = clampPercent(info.used_pct);
            const remainingPct = 100 - usedPct;
            const windowEnd = info.window_end;
            const windowType = info.window_type || '';
            const isExhausted = usedPct >= 100;

            return (
              <div
                key={idx}
                style={{
                  padding: '12px 16px',
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 8,
                  background: isExhausted ? 'var(--semi-color-danger-light-default, #fff5f5)' : 'transparent',
                }}
              >
                <div style={{ marginBottom: 8, display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <Text strong>{t('Key')} #{idx}</Text>
                  {isExhausted ? (
                    <Tag color='red'>{t('已耗尽')}</Tag>
                  ) : (
                    <Tag color='green'>{t('正常')}</Tag>
                  )}
                </div>

                <Progress
                  percent={usedPct}
                  strokeColor={getProgressColor(usedPct) === 'red' ? '#ef4444' : getProgressColor(usedPct) === 'amber' ? '#f59e0b' : '#22c55e'}
                  showInfo={true}
                />

                <div style={{ marginTop: 8, display: 'flex', justifyContent: 'space-between', fontSize: 13, color: 'var(--semi-color-text-2)' }}>
                  <span>
                    {t('已用')}: {usedPct.toFixed(1)}%
                  </span>
                  <span>
                    {t('剩余')}: {remainingPct.toFixed(1)}%
                  </span>
                </div>

                <div style={{ marginTop: 4, display: 'flex', gap: 16, fontSize: 13, color: 'var(--semi-color-text-2)' }}>
                  {windowEnd ? (
                    <span>
                      {t('重置于')}: {new Date(windowEnd * 1000).toLocaleString()}
                    </span>
                  ) : null}
                  {windowType ? (
                    <Tag size='small' color='light-blue'>{windowType === '5h' ? t('5小时') : windowType === 'weekly' ? t('周') : windowType}</Tag>
                  ) : null}
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div style={{ marginTop: 16, textAlign: 'right' }}>
        <Button type='primary' onClick={onClose}>{t('关闭')}</Button>
      </div>
    </div>
  );
};

export default openCodingPlanQuotaModal;
