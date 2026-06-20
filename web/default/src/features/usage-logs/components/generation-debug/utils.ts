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
import { formatBillingCurrencyFromUSD } from '@/lib/currency'

import type {
  CacheStatus,
  GenerationDebugMessage,
  GenerationDebugPromptUnit,
  GenerationDebugRawValue,
  PromptDebugData,
} from './types'

export function formatGenerationLatency(milliseconds: number): string {
  if (!Number.isFinite(milliseconds) || milliseconds <= 0) return '--'
  if (milliseconds < 1000) return `${Math.round(milliseconds)} ms`
  return `${(milliseconds / 1000).toFixed(2)} s`
}

export function formatGenerationThroughput(tokensPerSecond: number): string {
  if (!Number.isFinite(tokensPerSecond) || tokensPerSecond <= 0) return '--'
  return `${tokensPerSecond.toFixed(1)} tok/s`
}

export function formatGenerationCost(
  providerCost: unknown,
  chargedCost: number
): string {
  const cost = resolveCostValue(providerCost, chargedCost)
  if (!Number.isFinite(cost) || cost < 0) return '--'
  return formatBillingCurrencyFromUSD(cost, {
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  })
}

function resolveCostValue(providerCost: unknown, chargedCost: number): number {
  if (typeof providerCost === 'number') return providerCost
  if (typeof providerCost === 'string') {
    const parsed = Number(providerCost)
    if (Number.isFinite(parsed)) return parsed
  }
  return chargedCost
}

export function formatGenerationTokens(tokens: number): string {
  if (!Number.isFinite(tokens)) return '0'
  return Math.max(0, Math.round(tokens)).toLocaleString()
}

export function stringifyDebugValue(value: unknown): string {
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      return value
    }
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function roleCountsFromMessages(
  messages: GenerationDebugMessage[]
): Record<string, number> {
  return messages.reduce<Record<string, number>>((counts, message) => {
    const role = message.role || 'unknown'
    counts[role] = (counts[role] ?? 0) + 1
    return counts
  }, {})
}

export function roleVariant(
  role: string
): 'blue' | 'green' | 'purple' | 'amber' | 'neutral' {
  switch (role.toLowerCase()) {
    case 'assistant':
      return 'blue'
    case 'user':
      return 'green'
    case 'system':
    case 'developer':
      return 'purple'
    case 'tool':
    case 'function':
      return 'amber'
    default:
      return 'neutral'
  }
}

export function cacheStatusVariant(
  status: CacheStatus | string | undefined
): 'green' | 'amber' | 'neutral' | 'blue' | 'grey' | 'orange' {
  switch (status) {
    case 'hit':
      return 'green'
    case 'partial':
      return 'amber'
    case 'miss':
      return 'neutral'
    case 'write':
      return 'blue'
    default:
      return 'grey'
  }
}

export function cacheStatusLabel(
  status: CacheStatus | string | undefined
): string {
  if (status === 'hit') return 'hit'
  if (status === 'partial') return 'partial'
  if (status === 'miss') return 'miss'
  if (status === 'write') return 'write'
  return 'unknown'
}

export function normalizedPromptUnits(
  prompt: PromptDebugData | undefined
): GenerationDebugPromptUnit[] {
  if (prompt?.units && prompt.units.length > 0) return prompt.units
  let cumulative = 0
  return (prompt?.messages ?? []).map((message) => {
    const start = cumulative
    const estimatedTokens = message.estimated_tokens ?? 0
    cumulative += estimatedTokens
    return {
      index: message.index,
      message_index: message.index,
      path: `messages[${message.index}].content`,
      role: message.role,
      kind: 'text',
      content_preview: message.content,
      estimated_tokens: estimatedTokens,
      cumulative_start: start,
      cumulative_end: cumulative,
      cache_overlap_tokens: message.cached ? estimatedTokens : 0,
      cache_status: message.cached ? 'hit' : 'unknown',
      token_source: 'local_estimate',
      cache_source: message.cached ? 'legacy_message_flag' : 'legacy_message',
      confidence: message.cached ? 'inferred' : 'estimated',
    }
  })
}

export function rawValueContent(
  value: GenerationDebugRawValue | undefined
): unknown {
  return value?.value
}
