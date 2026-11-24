// modelExamples.js 模型操作对应的 SDK 与 HTTP 调用示例
// Builds operation-specific examples and rejects unavailable protocol templates.
// 版本: v0.0.28
// 日期: 2026-11-23
// 作者: Codex

const definitions = {
  'chat.generate': { path: '/chat/completions', method: 'chat.completions.create', payload: () => ({ messages: [{ role: 'user', content: 'Hello' }] }) },
  'image.generate': { path: '/images/generations', method: 'images.generate', payload: () => ({ prompt: 'A mountain at sunrise', n: 1, size: '1024x1024' }) },
  'audio.speech': { path: '/audio/speech', method: 'audio.speech.create', payload: name => ({ input: '你好，欢迎使用语音合成。', voice: /cosyvoice/.test(name) ? 'longanhuan_v3' : /qwen-audio/.test(name) ? 'longanhuan_v3.1' : 'alloy', response_format: 'wav', speed: 1 }) },
  'embedding.create': { path: '/embeddings', method: 'embeddings.create', payload: () => ({ input: 'A sample document' }) },
  'rerank.score': { path: '/rerank', payload: () => ({ query: 'What is a model gateway?', documents: ['A gateway routes model requests.', 'A mountain is a landform.'], top_n: 1, return_documents: true }) },
  'moderation.check': { path: '/moderations', method: 'moderations.create', payload: () => ({ input: 'A sample text to moderate' }) },
  'image.edit': { path: '/images/edits', payload: () => ({ prompt: 'Turn the sky blue', images: ['https://example.com/input.png'], n: 1 }) },
  'audio.transcribe': { path: '/audio/transcriptions', payload: () => ({ audio: 'https://example.com/audio.wav', response_format: 'json' }) },
  'video.generate': { path: '/media/jobs', payload: () => ({ operation: 'video.generate', prompt: 'A mountain at sunrise', duration: 5, size: '1280*720' }) },
}

const shellQuote = value => `'${String(value).replaceAll("'", "'\\''")}'`
const pythonValue = value => {
  if (value === null) return 'None'
  if (typeof value === 'boolean') return value ? 'True' : 'False'
  if (Array.isArray(value)) return `[${value.map(pythonValue).join(', ')}]`
  if (typeof value === 'object') return `{${Object.entries(value).map(([key, item]) => `${JSON.stringify(key)}: ${pythonValue(item)}`).join(', ')}}`
  return JSON.stringify(value)
}

// OPERATION_EXAMPLE_PRIORITY orders operations so the primary task wins the default selection.
// 版本: v0.0.28
// 日期: 2026-11-23
const OPERATION_EXAMPLE_PRIORITY = [
  'chat.generate', 'image.generate', 'audio.speech', 'embedding.create',
  'rerank.score', 'video.generate', 'audio.transcribe', 'moderation.check',
  'audio.translate', 'image.edit', 'image.variation',
]

// exampleSupported reports whether a concrete operation has a callable example template.
// 版本: v0.0.28
// 日期: 2026-11-23
export function exampleSupported(model = '', operation = '') {
  if (operation === 'audio.translate') return true
  if (operation === 'audio.transcribe' && !String(model).startsWith('qwen3-asr')) return true
  return Boolean(definitions[operation])
}

// orderExampleOperations keeps only operations with examples, primary task first.
// 版本: v0.0.28
// 日期: 2026-11-23
export function orderExampleOperations(model = '', operations = []) {
  const rank = op => {
    const idx = OPERATION_EXAMPLE_PRIORITY.indexOf(op)
    return idx === -1 ? OPERATION_EXAMPLE_PRIORITY.length : idx
  }
  return [...operations]
    .filter(op => exampleSupported(model, op))
    .sort((a, b) => rank(a) - rank(b))
}

// buildModelExample returns a valid operation template or an explicit unavailable state.
// 版本: v0.0.28
// 日期: 2026-11-23
export function buildModelExample({ model = '', operation = '', language = 'curl', baseUrl = 'https://your-gateway.example', apiKey = 'sk-your-key' } = {}) {
  if (operation === 'audio.translate' || operation === 'audio.transcribe' && !model.startsWith('qwen3-asr')) return uploadExample({ model, operation, language, baseUrl, apiKey })
  const definition = definitions[operation]
  if (!definition) return { supported: false, code: '', reason: 'No compatible example for this operation' }
  const root = baseUrl.replace(/\/$/, '').replace(/\/v1$/, '')
  const payload = { model, ...definition.payload(model) }
  if (operation === 'embedding.create' && model.startsWith('qwen3-vl-embedding')) {
    return rawExample({ root, apiKey, payload: { model, input: { contents: [{ text: 'A sample document' }] } }, path: '/multimodal/embeddings', language })
  }
  if (!definition.method) return rawExample({ root, apiKey, payload, path: definition.path, language })
  if (language === 'python') {
    const args = Object.entries(payload).map(([key, value]) => `    ${key}=${pythonValue(value)},`).join('\n')
    const output = operation === 'audio.speech' ? 'response.stream_to_file("speech.wav")' : 'print(response)'
    return { supported: true, code: `from openai import OpenAI\n\nclient = OpenAI(api_key=${JSON.stringify(apiKey)}, base_url=${JSON.stringify(root + '/v1')})\nresponse = client.${definition.method}(\n${args}\n)\n${output}` }
  }
  if (language === 'node') {
    const output = operation === 'audio.speech' ? 'await writeFile("speech.wav", Buffer.from(await response.arrayBuffer()));' : 'console.log(response);'
    return { supported: true, code: `import OpenAI from "openai";\n${operation === 'audio.speech' ? 'import { writeFile } from "node:fs/promises";\n' : ''}\nconst client = new OpenAI({ apiKey: ${JSON.stringify(apiKey)}, baseURL: ${JSON.stringify(root + '/v1')} });\nconst response = await client.${definition.method}(${JSON.stringify(payload, null, 2)});\n${output}` }
  }
  return rawExample({ root, apiKey, payload, path: definition.path, language })
}

function uploadExample({ model, operation, language, baseUrl, apiKey }) {
  const root = baseUrl.replace(/\/$/, '').replace(/\/v1$/, '')
  const kind = operation === 'audio.translate' ? 'translations' : 'transcriptions'
  if (language === 'python') return { supported: true, code: `from openai import OpenAI\n\nclient = OpenAI(api_key=${JSON.stringify(apiKey)}, base_url=${JSON.stringify(root + '/v1')})\nwith open("audio.wav", "rb") as audio:\n    response = client.audio.${kind}.create(model=${JSON.stringify(model)}, file=audio)\nprint(response.text)` }
  if (language === 'node') return { supported: true, code: `import OpenAI from "openai";\nimport { createReadStream } from "node:fs";\n\nconst client = new OpenAI({ apiKey: ${JSON.stringify(apiKey)}, baseURL: ${JSON.stringify(root + '/v1')} });\nconst response = await client.audio.${kind}.create({ model: ${JSON.stringify(model)}, file: createReadStream("audio.wav") });\nconsole.log(response.text);` }
  return { supported: true, code: `curl --fail-with-body ${shellQuote(root + '/v1/audio/' + kind)} \\\n  -H ${shellQuote('Authorization: Bearer ' + apiKey)} \\\n  -F ${shellQuote('model=' + model)} \\\n  -F 'file=@audio.wav'` }
}

function rawExample({ root, apiKey, payload, path, language }) {
  const url = root + '/v1' + path
  const job = path === '/media/jobs'
  const headers = { Authorization: 'Bearer ' + apiKey, 'Content-Type': 'application/json', ...(job ? { 'Idempotency-Key': 'e4858859-9c80-41d2-a333-9436f9a75b22' } : {}) }
  const note = job ? '# Use a new Idempotency-Key for each generation; reuse it only for retries.\n' : ''
  if (language === 'python') return { supported: true, code: `import requests\n\n${note}response = requests.post(${JSON.stringify(url)}, headers=${pythonValue(headers)}, json=${pythonValue(payload)})\nresponse.raise_for_status()\nprint(response.json())` }
  if (language === 'node') return { supported: true, code: `${note.replace('#', '//')}const response = await fetch(${JSON.stringify(url)}, {\n  method: "POST",\n  headers: ${JSON.stringify(headers)},\n  body: JSON.stringify(${JSON.stringify(payload, null, 2)})\n});\nif (!response.ok) throw new Error(await response.text());\nconsole.log(await response.json());` }
  const headerLines = Object.entries(headers).map(([key, value]) => `  -H ${shellQuote(key + ': ' + value)}`).join(' \\\n')
  return { supported: true, code: `${note}curl --fail-with-body ${shellQuote(url)} \\\n${headerLines} \\\n  -d ${shellQuote(JSON.stringify(payload, null, 2))}${path === '/audio/speech' ? ' \\\n  -o speech.wav' : ''}` }
}
