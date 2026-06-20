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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Switch } from '@/components/ui/switch'
import { StatusBadge } from '@/components/status-badge'
import { JsonViewer } from './json-viewer'
import { TokenMessageChart } from './token-message-chart'
import type {
  GenerationDebugRawValue,
  PromptDebugData,
} from './types'
import {
  formatGenerationTokens,
  roleCountsFromMessages,
  roleVariant,
} from './utils'

interface PromptDebugPanelProps {
  prompt: PromptDebugData | undefined
  rawRequest: GenerationDebugRawValue | undefined
}

export function PromptDebugPanel(props: PromptDebugPanelProps) {
  const { t } = useTranslation()
  const [showRawRequest, setShowRawRequest] = useState(false)
  const messages = useMemo(
    () => props.prompt?.messages ?? [],
    [props.prompt?.messages]
  )
  const roleCounts = useMemo(
    () =>
      Object.keys(props.prompt?.role_counts ?? {}).length > 0
        ? (props.prompt?.role_counts ?? {})
        : roleCountsFromMessages(messages),
    [messages, props.prompt?.role_counts]
  )

  if (!props.prompt && !props.rawRequest) {
    return <p className='text-muted-foreground text-xs'>{t('No prompt data')}</p>
  }

  return (
    <div className='flex min-w-0 flex-col gap-3'>
      {messages.length > 0 && <TokenMessageChart messages={messages} />}

      <div className='grid min-w-0 gap-2 sm:grid-cols-2'>
        <div className='bg-muted/30 flex min-w-0 flex-col gap-1.5 rounded-md border p-2.5'>
          <span className='text-muted-foreground text-[11px]'>
            {t('Estimated prompt tokens')}
          </span>
          <span className='font-mono text-sm font-semibold'>
            {formatGenerationTokens(
              props.prompt?.total_estimated_tokens ?? 0
            )}
          </span>
        </div>
        <div className='bg-muted/30 flex min-w-0 flex-col gap-1.5 rounded-md border p-2.5'>
          <span className='text-muted-foreground text-[11px]'>
            {t('Role counts')}
          </span>
          <div className='flex flex-wrap gap-1.5'>
            {Object.entries(roleCounts).map(([role, count]) => (
              <StatusBadge
                key={role}
                label={`${role} · ${count}`}
                variant={roleVariant(role)}
                size='sm'
                copyable={false}
              />
            ))}
          </div>
        </div>
      </div>

      {props.rawRequest && (
        <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2'>
          <Label htmlFor='generation-debug-show-raw-request'>
            {t('Show raw request')}
          </Label>
          <Switch
            id='generation-debug-show-raw-request'
            checked={showRawRequest}
            onCheckedChange={setShowRawRequest}
            size='sm'
          />
        </div>
      )}

      {showRawRequest && props.rawRequest ? (
        <JsonViewer
          value={props.rawRequest.value}
          rawMeta={props.rawRequest}
          maxHeightClassName='h-72'
        />
      ) : (
        messages.length > 0 && (
          <ScrollArea className='h-72 rounded-md border'>
            <div className='flex min-w-0 flex-col divide-y'>
              {messages.map((message) => (
                <div
                  key={`${message.index}-${message.role}`}
                  className='flex min-w-0 flex-col gap-2 p-3'
                >
                  <div className='flex flex-wrap items-center justify-between gap-2'>
                    <div className='flex items-center gap-2'>
                      <span className='text-muted-foreground font-mono text-[11px]'>
                        #{message.index + 1}
                      </span>
                      <StatusBadge
                        label={message.role || t('Unknown')}
                        variant={roleVariant(message.role)}
                        size='sm'
                        copyable={false}
                      />
                      {message.cached && (
                        <StatusBadge
                          label={t('Cached')}
                          variant='grey'
                          size='sm'
                          copyable={false}
                        />
                      )}
                    </div>
                    <span className='text-muted-foreground text-[11px]'>
                      ~{formatGenerationTokens(message.estimated_tokens)}{' '}
                      {t('tokens')}
                    </span>
                  </div>
                  <p className='text-xs leading-relaxed break-words whitespace-pre-wrap'>
                    {message.content || t('No text content')}
                  </p>
                </div>
              ))}
            </div>
          </ScrollArea>
        )
      )}
    </div>
  )
}
