import assert from 'node:assert/strict'

import { describe, test } from 'vitest'

import {
  promptFieldDisplayText,
  promptFieldTextFromRawRequest,
  stringifyDebugValue,
} from './utils'

describe('generation debug stringifyDebugValue', () => {
  test('formats valid JSON strings', () => {
    assert.equal(
      stringifyDebugValue('{"messages":[{"role":"user","content":"hi"}]}'),
      [
        '{',
        '  "messages": [',
        '    {',
        '      "role": "user",',
        '      "content": "hi"',
        '    }',
        '  ]',
        '}',
      ].join('\n')
    )
  })

  test('formats truncated JSON-like strings for visual inspection', () => {
    assert.equal(
      stringifyDebugValue('{"messages":[{"role":"user","content":"hi"},'),
      [
        '{',
        '  "messages": [',
        '    {',
        '      "role": "user",',
        '      "content": "hi"',
        '    },',
        '    ',
      ].join('\n')
    )
  })

  test('keeps non-JSON strings unchanged', () => {
    assert.equal(stringifyDebugValue('data: [DONE]'), 'data: [DONE]')
  })

  test('reads full prompt field content from raw request paths', () => {
    assert.equal(
      promptFieldTextFromRawRequest(
        {
          value: {
            messages: [
              {
                content: [
                  {
                    type: 'text',
                    text: 'full selected prompt text',
                  },
                ],
              },
            ],
          },
          truncated: false,
          captured_bytes: 80,
        },
        'messages[0].content[0].text'
      ),
      'full selected prompt text'
    )
  })

  test('formats non-string raw request field values', () => {
    assert.equal(
      promptFieldTextFromRawRequest(
        {
          value: { response_format: { type: 'json_object' } },
          truncated: false,
          captured_bytes: 48,
        },
        'response_format'
      ),
      ['{', '  "type": "json_object"', '}'].join('\n')
    )
  })

  test('uses full prompt unit content before preview fallback', () => {
    assert.equal(
      promptFieldDisplayText(undefined, {
        index: 0,
        message_index: 0,
        path: 'messages[0].content',
        kind: 'text',
        content: 'full content',
        content_preview: 'full...',
        estimated_tokens: 3,
        cumulative_start: 0,
        cumulative_end: 3,
        cache_overlap_tokens: 0,
        cache_status: 'unknown',
        token_source: 'local_estimate',
        cache_source: 'cache_boundary_inference',
        confidence: 'estimated',
      }),
      'full content'
    )
  })
})
