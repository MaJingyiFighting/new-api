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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { ENDPOINT_TEMPLATES } from '../../models/constants'
import { ModelDetailsApi } from '../components/model-details-api'

const clients: QueryClient[] = []

afterEach(() => {
  clients.splice(0).forEach((client) => client.clear())
  localStorage.clear()
  vi.restoreAllMocks()
})

it.each([
  ['typesafe-decisions', 'jev-latest', '/v1/systemone'],
  [
    'openrouter-decisions',
    'openai/gpt-6-luna-decisions',
    '/v1/alpha/decisions',
  ],
])(
  'shows usable %s requests in each language without chat fields or invented rate limits',
  async (endpoint, model, path) => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: true, data: {} },
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    clients.push(client)
    render(
      <QueryClientProvider client={client}>
        <ModelDetailsApi
          model={{
            id: 1,
            model_name: model,
            quota_type: 0,
            model_ratio: 1,
            completion_ratio: 0,
            enable_groups: ['default'],
            group_ratio: { default: 1 },
            supported_endpoint_types: [endpoint],
          }}
          endpointMap={ENDPOINT_TEMPLATES}
        />
      </QueryClientProvider>
    )
    for (const language of ['cURL', 'Python', 'TypeScript', 'JavaScript']) {
      await userEvent.click(screen.getByRole('tab', { name: language }))
      await waitFor(() => {
        const code = screen.getByRole('textbox', {
          name: language === 'cURL' ? 'bash' : language.toLowerCase(),
        }).textContent
        expect(code).toContain(path)
        expect(code).toContain(model)
        expect(code).toContain('DECISION_STATE')
        expect(code).toContain('DECISION_QUESTION')
        expect(code).toContain('questions')
        expect(code).toContain('noul')
        expect(code).not.toContain('messages')
        expect(code).not.toContain('temperature')
        expect(code).not.toContain('<YOUR_API_KEY>')
      })
    }
    expect(screen.queryByText('Rate limits')).not.toBeInTheDocument()
    expect(screen.queryByText('Supported parameters')).not.toBeInTheDocument()
  }
)
