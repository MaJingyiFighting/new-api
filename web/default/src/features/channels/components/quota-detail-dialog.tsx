/*
Copyright (C) 2023-2026 QuantumNous

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
import { useEffect, useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Loader2, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Dialog } from '@/components/dialog'
import { StatusBadge, type StatusBadgeProps } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyTitle,
} from '@/components/ui/empty'
import { Progress } from '@/components/ui/progress'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import { queryChannelQuota } from '../api'
import { channelsQueryKeys } from '../lib'
import { formatQuotaWindowCountdown, getQuotaVariant } from '../lib/channel-utils'
import type { CodingPlanKeyQuota } from '../types'

export interface QuotaDetailDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelId: number
  channelName: string
}

function statusVariantFor(pct: number): StatusBadgeProps['variant'] {
  if (pct >= 85) {
    return 'danger'
  }
  if (pct >= 60) {
    return 'warning'
  }
  return 'success'
}

function windowTypeLabel(type: string | undefined, t: (k: string) => string) {
  if (!type) {
    return '-'
  }
  if (type === '5h') {
    return t('5h')
  }
  if (type === 'weekly') {
    return t('Weekly')
  }
  if (type === 'monthly') {
    return t('Monthly')
  }
  return type
}

/**
 * Modal showing per-key coding-plan quota usage, remaining time until the next
 * window reset, and a healthy/exhausted status. Triggered by the quota badge
 * in the channels list.
 */
export function QuotaDetailDialog({
  open,
  onOpenChange,
  channelId,
  channelName,
}: QuotaDetailDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState('')
  const [quotaInfo, setQuotaInfo] = useState<Record<
    string,
    CodingPlanKeyQuota
  > | null>(null)
  // Tick once per minute to keep the live countdown fresh.
  const [, setTick] = useState(0)

  useEffect(() => {
    if (!open) {
      return
    }
    const id = window.setInterval(() => {
      setTick((n) => n + 1)
    }, 30_000)
    return () => window.clearInterval(id)
  }, [open])

  const refresh = async () => {
    setIsLoading(true)
    setError('')
    try {
      const res = await queryChannelQuota(channelId)
      if (!res.success) {
        throw new Error(res.message || t('Failed to query quota'))
      }
      setQuotaInfo(res.quota_info ?? {})
      toast.success(t('Quota refreshed'))
      // Channels list caches don't carry the live quota map (the per-key
      // window state is fetched on demand), but invalidation keeps the table
      // in sync if the backend ever returns quota data on the list endpoint.
      void queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : t('Failed to query quota')
      setError(msg)
      toast.error(msg)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    if (!open) {
      return
    }
    void refresh()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, channelId])

  const rows = useMemo(() => {
    if (!quotaInfo) {
      return [] as Array<{ key: string; idx: number; data: CodingPlanKeyQuota }>
    }
    return Object.entries(quotaInfo)
      .map(([key, data], idx) => ({ key, idx, data }))
      .sort((a, b) => a.idx - b.idx)
  }, [quotaInfo])

  const firstType = rows[0]?.data.window_type ?? ''
  const latestEnd = rows.reduce(
    (max, r) => (r.data.window_end > max ? r.data.window_end : max),
    0
  )
  const resetAtText = latestEnd ? formatTimestampToDate(latestEnd) : '-'

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Quota Details')}
      description={
        <>
          {t('Query Coding Plan Quota for')}{' '}
          <strong>{channelName}</strong>
        </>
      }
      contentHeight='auto'
      bodyClassName='flex flex-col gap-4'
      footer={
        <>
          <Button
            variant='outline'
            onClick={refresh}
            disabled={isLoading}
          >
            {isLoading ? (
              <Loader2 className='mr-2 h-4 w-4 animate-spin' />
            ) : (
              <RefreshCw className='mr-2 h-4 w-4' />
            )}
            {t('Refresh')}
          </Button>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
        </>
      }
    >
      <div className='bg-muted/40 flex flex-wrap items-center gap-2 rounded-lg p-3 text-sm'>
        <div>
          <span className='text-muted-foreground mr-1'>{t('Window')}:</span>
          <span className='font-medium'>{windowTypeLabel(firstType, t)}</span>
        </div>
        <div>
          <span className='text-muted-foreground mr-1'>{t('Reset at')}:</span>
          <span className='font-mono tabular-nums'>{resetAtText}</span>
        </div>
        <div>
          <span className='text-muted-foreground mr-1'>{t('Resets in')}:</span>
          <span className='font-mono tabular-nums'>
            {formatQuotaWindowCountdown(latestEnd)}
          </span>
        </div>
      </div>

      {error ? (
        <div className='border-destructive/40 bg-destructive/10 text-destructive rounded-lg border px-3 py-2 text-sm'>
          {error}
        </div>
      ) : null}

      {isLoading && rows.length === 0 ? (
        <div className='text-muted-foreground flex items-center gap-2 py-6 text-sm'>
          <Loader2 className='h-4 w-4 animate-spin' />
          {t('Querying...')}
        </div>
      ) : rows.length === 0 ? (
        <Empty className='min-h-32 border'>
          <EmptyHeader>
            <EmptyTitle>{t('No quota data')}</EmptyTitle>
            <EmptyDescription>
              {t('Upstream did not return any quota windows.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className='overflow-x-auto rounded-lg border'>
          <table className='w-full text-sm'>
            <thead className='bg-muted/40 text-muted-foreground text-xs'>
              <tr>
                <th className='px-3 py-2 text-left font-medium'>#</th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Used')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Resets in')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Reset at')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Window')}
                </th>
                <th className='px-3 py-2 text-left font-medium'>
                  {t('Status')}
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map(({ key, idx, data }) => {
                const pct = Number.isFinite(data.used_pct)
                  ? Math.max(0, Math.min(100, data.used_pct))
                  : 0
                const exhausted = pct >= 85
                const variant = getQuotaVariant(pct)
                return (
                  <tr
                    key={key}
                    className='border-t transition-colors hover:bg-muted/30'
                  >
                    <td className='px-3 py-2 font-mono text-xs'>
                      {t('Key {{index}}', { index: idx })}
                    </td>
                    <td className='px-3 py-2'>
                      <div className='flex items-center gap-2'>
                        <span
                          className={cn(
                            'min-w-[3rem] font-semibold tabular-nums',
                            variant === 'green' && 'text-success',
                            variant === 'yellow' && 'text-warning',
                            variant === 'red' && 'text-destructive'
                          )}
                        >
                          {pct.toFixed(0)}%
                        </span>
                        <Progress
                          value={pct}
                          aria-label={`Key ${idx} usage`}
                          className='min-w-[100px]'
                        />
                      </div>
                    </td>
                    <td className='px-3 py-2 font-mono text-xs tabular-nums'>
                      {formatQuotaWindowCountdown(data.window_end)}
                    </td>
                    <td className='px-3 py-2 font-mono text-xs tabular-nums'>
                      {data.window_end
                        ? formatTimestampToDate(data.window_end)
                        : '-'}
                    </td>
                    <td className='px-3 py-2 text-xs'>
                      {windowTypeLabel(data.window_type, t)}
                    </td>
                    <td className='px-3 py-2'>
                      <StatusBadge
                        label={exhausted ? t('Exhausted') : t('Healthy')}
                        variant={statusVariantFor(pct)}
                        copyable={false}
                      />
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </Dialog>
  )
}
