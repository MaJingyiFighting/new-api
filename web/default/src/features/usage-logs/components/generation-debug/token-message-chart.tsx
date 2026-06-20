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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Bar,
  BarChart,
  Cell,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { GenerationDebugMessage } from './types'

interface TokenMessageChartProps {
  messages: GenerationDebugMessage[]
}

function roleColor(role: string): string {
  switch (role.toLowerCase()) {
    case 'assistant':
      return 'var(--chart-1)'
    case 'user':
      return 'var(--chart-2)'
    case 'system':
    case 'developer':
      return 'var(--chart-4)'
    case 'tool':
    case 'function':
      return 'var(--chart-3)'
    default:
      return 'var(--muted-foreground)'
  }
}

export function TokenMessageChart(props: TokenMessageChartProps) {
  const { t } = useTranslation()
  const data = useMemo(
    () =>
      props.messages.map((message) => ({
        index: message.index + 1,
        tokens: message.estimated_tokens,
        role: message.role || 'unknown',
        cached: message.cached,
      })),
    [props.messages]
  )

  if (data.length === 0) return null

  return (
    <div className='flex min-w-0 flex-col gap-2 rounded-md border p-2.5'>
      <div className='flex flex-wrap items-baseline justify-between gap-2'>
        <span className='text-xs font-semibold'>{t('Tokens per message')}</span>
        <span className='text-muted-foreground text-[11px]'>
          {t('Estimated values, not used for billing')}
        </span>
      </div>
      <div className='h-28 min-w-0'>
        <ResponsiveContainer width='100%' height='100%'>
          <BarChart
            data={data}
            margin={{ top: 4, right: 2, bottom: 0, left: 2 }}
          >
            <XAxis dataKey='index' hide />
            <YAxis hide />
            <Tooltip
              cursor={{ fill: 'var(--muted)', opacity: 0.45 }}
              formatter={(value, _name, item) => [
                `${Number(value).toLocaleString()} ${t('tokens')} (${t('estimated')})`,
                String(item.payload.role),
              ]}
              labelFormatter={(value) => `${t('Message')} #${value}`}
              contentStyle={{
                borderRadius: '8px',
                borderColor: 'var(--border)',
                backgroundColor: 'var(--background)',
                fontSize: '12px',
              }}
            />
            <Bar dataKey='tokens' radius={[2, 2, 0, 0]} minPointSize={2}>
              {data.map((entry) => (
                <Cell
                  key={`${entry.index}-${entry.role}`}
                  fill={roleColor(entry.role)}
                  fillOpacity={entry.cached ? 0.35 : 0.9}
                />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
      <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-[11px]'>
        {[...new Set(data.map((entry) => entry.role))].map((role) => (
          <span key={role} className='flex items-center gap-1'>
            <span
              className='size-2 rounded-full'
              style={{ backgroundColor: roleColor(role) }}
              aria-hidden='true'
            />
            {role}
          </span>
        ))}
        {data.some((entry) => entry.cached) && <span>{t('Faded = cached')}</span>}
      </div>
    </div>
  )
}
