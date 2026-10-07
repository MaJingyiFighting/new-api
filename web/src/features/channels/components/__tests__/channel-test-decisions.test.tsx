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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, assert, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { channelSchema, type Channel } from '../../types'
import { ChannelsProvider, useChannels } from '../channels-provider'
import { ChannelTestDialog } from '../dialogs/channel-test-dialog'

const clients: QueryClient[] = []

function TestChannel(props: { channel: Channel }) {
  const channels = useChannels()
  return (
    <>
      <button
        type='button'
        onClick={() => channels.setCurrentRow(props.channel)}
      >
        Open test
      </button>
      <ChannelTestDialog open onOpenChange={() => undefined} />
    </>
  )
}

async function renderChannel(overrides: Partial<Channel>) {
  const get = vi.spyOn(api, 'get').mockResolvedValue({
    data: { success: true, time: 0.1, data: [] },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  clients.push(client)
  const channel = channelSchema.parse({
    id: 3,
    type: 20,
    key: '',
    name: 'Decisions channel',
    status: 1,
    created_time: 0,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
    models: 'openai/gpt-6-luna-decisions',
    ...overrides,
  })
  render(
    <QueryClientProvider client={client}>
      <ChannelsProvider>
        <TestChannel channel={channel} />
      </ChannelsProvider>
    </QueryClientProvider>
  )
  await userEvent.click(screen.getByRole('button', { name: 'Open test' }))
  await screen.findByRole('dialog')
  return get
}

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  localStorage.clear()
  vi.restoreAllMocks()
})

it.each([
  { type: 1, models: 'jev-latest' },
  { type: 20, models: 'openai/gpt-6-luna-decisions' },
])(
  'disables streaming when all auto-detected channel models use Decisions: $models',
  async (channel) => {
    await renderChannel(channel)
    expect(screen.getByRole('switch', { name: 'Stream Mode' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
  }
)

it('tests a mapped Decisions model without streaming while retaining streaming for a chat model', async () => {
  const get = await renderChannel({
    models: 'judge,openai/gpt-6-luna',
    model_mapping: '{"judge":"openai/gpt-6-luna-decisions"}',
  })
  await userEvent.click(screen.getByRole('switch', { name: 'Stream Mode' }))
  const judgeRow = screen.getByText('judge').closest('tr')
  assert(judgeRow)
  await userEvent.click(
    within(judgeRow).getByRole('button', { name: 'Test Connection' })
  )
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/api/channel/test/3',
      expect.objectContaining({
        params: { model: 'judge', endpoint_type: 'openrouter-decisions' },
      })
    )
  )
  const chatRow = screen.getByText('openai/gpt-6-luna').closest('tr')
  assert(chatRow)
  await userEvent.click(
    within(chatRow).getByRole('button', { name: 'Test Connection' })
  )
  await waitFor(() =>
    expect(get).toHaveBeenCalledWith(
      '/api/channel/test/3',
      expect.objectContaining({
        params: { model: 'openai/gpt-6-luna', stream: true },
      })
    )
  )
})

it.each([
  ['TypeSafe Decisions (/v1/systemone)', 'typesafe-decisions'],
  ['OpenRouter Decisions (/v1/alpha/decisions)', 'openrouter-decisions'],
])(
  'turns off streaming when choosing %s for an unrecognized model name',
  async (label, endpoint) => {
    const get = await renderChannel({ models: 'judge' })
    await userEvent.click(screen.getByRole('switch', { name: 'Stream Mode' }))
    await userEvent.click(
      screen.getByRole('combobox', { name: 'Endpoint Type' })
    )
    await userEvent.click(await screen.findByRole('option', { name: label }))
    expect(screen.getByRole('switch', { name: 'Stream Mode' })).toHaveAttribute(
      'aria-disabled',
      'true'
    )
    expect(screen.getByRole('switch', { name: 'Stream Mode' })).toHaveAttribute(
      'aria-checked',
      'false'
    )
    await userEvent.click(
      screen.getByRole('button', { name: 'Test Connection' })
    )
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith(
        '/api/channel/test/3',
        expect.objectContaining({
          params: { model: 'judge', endpoint_type: endpoint },
        })
      )
    )
  }
)
