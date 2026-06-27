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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import {
  formatQuotaWindowCountdown,
  getQuotaVariant,
  type QuotaVariant,
} from '../lib/channel-utils'

export interface QuotaWindowBadgeProps {
  usedPct: number
  windowType: string
  windowEnd: number
  totalKeys?: number
  healthyKeys?: number
  onClick?: () => void
  className?: string
}

const VARIANT_BAR: Record<QuotaVariant, string> = {
  green: 'bg-success',
  yellow: 'bg-warning',
  red: 'bg-destructive',
}

const VARIANT_TEXT: Record<QuotaVariant, string> = {
  green: 'text-success',
  yellow: 'text-warning',
  red: 'text-destructive',
}

const TICK_MS = 1000

/**
 * Compact coding-plan quota badge: usage percentage, window type, live
 * countdown, and (for multi-key channels) the count of healthy keys.
 * Renders a colored progress bar plus a clickable button area.
 */
export function QuotaWindowBadge({
  usedPct,
  windowType,
  windowEnd,
  totalKeys,
  healthyKeys,
  onClick,
  className,
}: QuotaWindowBadgeProps) {
  const { t } = useTranslation()
  const [nowSec, setNowSec] = useState(() => Math.floor(Date.now() / 1000))

  useEffect(() => {
    if (!windowEnd) {
      return
    }
    const id = window.setInterval(() => {
      setNowSec(Math.floor(Date.now() / 1000))
    }, TICK_MS)
    return () => window.clearInterval(id)
  }, [windowEnd])

  const clamped = Math.max(0, Math.min(100, usedPct))
  const variant = getQuotaVariant(clamped)
  const countdown = windowEnd
    ? formatQuotaWindowCountdown(windowEnd)
    : '-'
  void nowSec // re-render trigger

  const typeLabel = (() => {
    if (!windowType) {
      return ''
    }
    if (windowType === '5h') {
      return t('5h')
    }
    if (windowType === 'weekly') {
      return t('Weekly')
    }
    if (windowType === 'monthly') {
      return t('Monthly')
    }
    return windowType
  })()

  const showKeySummary =
    typeof totalKeys === 'number' &&
    totalKeys > 0 &&
    typeof healthyKeys === 'number'

  return (
    <button
      type='button'
      onClick={onClick}
      className={cn(
        'border-border/60 bg-muted/40 hover:bg-muted/70 -ml-1.5 flex w-full min-w-[150px] flex-col gap-1 rounded-md border px-2 py-1 text-left transition-colors',
        onClick && 'cursor-pointer',
        !onClick && 'cursor-default',
        className
      )}
      title={t('Query Coding Plan Quota')}
    >
      <div className='flex items-center justify-between gap-1.5 text-xs'>
        <span
          className={cn('font-semibold tabular-nums', VARIANT_TEXT[variant])}
        >
          {clamped.toFixed(0)}%
        </span>
        <span className='text-muted-foreground truncate'>{typeLabel}</span>
        <span className='text-muted-foreground tabular-nums'>{countdown}</span>
      </div>
      <div className='bg-muted-foreground/20 relative h-1.5 w-full overflow-hidden rounded-full'>
        <div
          className={cn('h-full transition-all', VARIANT_BAR[variant])}
          style={{ width: `${clamped}%` }}
        />
      </div>
      {showKeySummary && (
        <div className='text-muted-foreground flex items-center justify-between text-[10px]'>
          <span>
            {t('Healthy keys')}: {healthyKeys}/{totalKeys}
          </span>
        </div>
      )}
    </button>
  )
}
