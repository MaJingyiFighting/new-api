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

export function formatTokens(value) {
  const number = Number(value);
  if (!Number.isFinite(number)) return '0';
  return Math.max(0, Math.round(number)).toLocaleString();
}

export function formatLatency(milliseconds) {
  const number = Number(milliseconds);
  if (!Number.isFinite(number) || number <= 0) return '--';
  if (number < 1000) return `${Math.round(number)} ms`;
  return `${(number / 1000).toFixed(2)} s`;
}

export function formatThroughput(tokensPerSecond) {
  const number = Number(tokensPerSecond);
  if (!Number.isFinite(number) || number <= 0) return '--';
  return `${number.toFixed(1)} tok/s`;
}

export function formatCost(providerCost, chargedCost) {
  const cost =
    typeof providerCost === 'number'
      ? providerCost
      : typeof providerCost === 'string'
        ? Number(providerCost)
        : chargedCost;
  if (!Number.isFinite(cost) || cost < 0) return '--';
  return `$${cost.toFixed(cost < 0.01 ? 6 : 4)}`;
}

export function stringifyDebugValue(value) {
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2);
    } catch {
      return value;
    }
  }
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

export function normalizedPromptUnits(prompt) {
  if (prompt?.units?.length > 0) return prompt.units;
  let cumulative = 0;
  return (prompt?.messages ?? []).map((message) => {
    const estimatedTokens = message.estimated_tokens ?? 0;
    const start = cumulative;
    cumulative += estimatedTokens;
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
    };
  });
}

export function roleCountsFromMessages(messages = []) {
  return messages.reduce((counts, message) => {
    const role = message.role || 'unknown';
    counts[role] = (counts[role] ?? 0) + 1;
    return counts;
  }, {});
}

export function cacheStatusColor(status) {
  if (status === 'hit') return 'green';
  if (status === 'partial') return 'orange';
  if (status === 'miss') return 'grey';
  if (status === 'write') return 'blue';
  return 'light-blue';
}

export function cacheStatusBackground(status) {
  if (status === 'hit') return 'var(--semi-color-success)';
  if (status === 'partial') return 'var(--semi-color-warning)';
  if (status === 'miss') return 'var(--semi-color-tertiary)';
  if (status === 'write') return 'var(--semi-color-info)';
  return 'var(--semi-color-info-light-default)';
}
