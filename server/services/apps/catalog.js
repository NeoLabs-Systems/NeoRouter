'use strict';

// Apps people connect to NeoRouter, as data: what each app calls its fields
// (in its own words, so they can be found on screen), which API it speaks,
// where it usually runs, and the config file it reads, when it has one.
// Adding an app is adding an entry. Templates use {{baseUrl}} (ends in /v1),
// {{rootUrl}} (without /v1), {{apiKey}} and {{model}}.
const { z } = require('zod');

const text = z.object({ en: z.string().min(1), de: z.string().min(1) });
const field = z.object({
  label: z.string().min(1),
  value: z.enum(['baseUrl', 'rootUrl', 'apiKey', 'model']),
});
const app = z.object({
  id: z.string().regex(/^[a-z0-9-]+$/),
  name: z.string().min(1),
  group: z.enum(['coding', 'chat', 'automation', 'developer']),
  api: z.enum(['openai', 'anthropic']),
  summary: text,
  // Where it usually runs: preselected in the assistant.
  runs: z.enum(['local', 'docker', 'network', 'internet']).default('local'),
  // The app calls its servers, not yours: only an address on the internet works.
  cloudOnly: z.boolean().default(false),
  steps: z.array(text).min(1),
  fields: z.array(field).default([]),
  file: z.object({ name: z.string().min(1), language: z.string().min(1), template: z.string().min(1) }).optional(),
  website: z.string().url().optional(),
});

const OPENAI_FIELDS = [
  { label: 'Base URL', value: 'baseUrl' },
  { label: 'API Key', value: 'apiKey' },
  { label: 'Model', value: 'model' },
];

const APPS = [
  {
    id: 'pi-dev', name: 'Pi Dev', group: 'coding', api: 'openai', runs: 'docker',
    summary: { en: 'Coding agent harness.', de: 'Coding-Agent-Umgebung.' },
    steps: [
      { en: 'Open Pi Dev’s models.json.', de: 'Öffne die models.json von Pi Dev.' },
      { en: 'Replace its content with the file below, or add the "neorouter" provider to it.', de: 'Ersetze den Inhalt durch die Datei unten oder füge den Anbieter „neorouter“ hinzu.' },
      { en: 'Pick the model "{{model}}" in Pi Dev.', de: 'Wähle in Pi Dev das Modell „{{model}}“.' },
    ],
    file: {
      name: 'models.json', language: 'json',
      template: '{\n  "$schema": "https://pi.dev/schemas/models.schema.json",\n  "providers": {\n    "neorouter": {\n      "baseUrl": "{{baseUrl}}",\n      "api": "openai-completions",\n      "apiKey": "{{apiKey}}",\n      "models": [{ "id": "{{model}}" }]\n    }\n  }\n}\n',
    },
  },
  {
    id: 'cline', name: 'Cline', group: 'coding', api: 'openai',
    summary: { en: 'Coding agent in VS Code.', de: 'Coding-Agent in VS Code.' },
    steps: [
      { en: 'Open Cline’s settings (the gear in its panel).', de: 'Öffne die Einstellungen von Cline (Zahnrad im Cline-Fenster).' },
      { en: 'Set "API Provider" to "OpenAI Compatible".', de: 'Stelle „API Provider“ auf „OpenAI Compatible“.' },
      { en: 'Fill in the three fields below and save.', de: 'Trage die drei Felder unten ein und speichere.' },
    ],
    fields: [{ label: 'Base URL', value: 'baseUrl' }, { label: 'API Key', value: 'apiKey' }, { label: 'Model ID', value: 'model' }],
  },
  {
    id: 'roo-code', name: 'Roo Code', group: 'coding', api: 'openai',
    summary: { en: 'Coding agent in VS Code.', de: 'Coding-Agent in VS Code.' },
    steps: [
      { en: 'Open Roo Code’s settings.', de: 'Öffne die Einstellungen von Roo Code.' },
      { en: 'Set "API Provider" to "OpenAI Compatible".', de: 'Stelle „API Provider“ auf „OpenAI Compatible“.' },
      { en: 'Fill in the fields below and save.', de: 'Trage die Felder unten ein und speichere.' },
    ],
    fields: [{ label: 'Base URL', value: 'baseUrl' }, { label: 'API Key', value: 'apiKey' }, { label: 'Model', value: 'model' }],
  },
  {
    id: 'continue', name: 'Continue', group: 'coding', api: 'openai',
    summary: { en: 'AI assistant in VS Code and JetBrains.', de: 'KI-Assistent in VS Code und JetBrains.' },
    steps: [
      { en: 'Open Continue’s config.yaml (gear icon → "Open config file").', de: 'Öffne die config.yaml von Continue (Zahnrad → „Open config file“).' },
      { en: 'Add the model below under "models".', de: 'Füge das Modell unten unter „models“ hinzu.' },
    ],
    file: {
      name: 'config.yaml', language: 'yaml',
      template: 'models:\n  - name: NeoRouter\n    provider: openai\n    model: {{model}}\n    apiBase: {{baseUrl}}\n    apiKey: {{apiKey}}\n',
    },
  },
  {
    id: 'aider', name: 'Aider', group: 'coding', api: 'openai',
    summary: { en: 'Pair programming in the terminal.', de: 'Pair-Programming im Terminal.' },
    steps: [{ en: 'Start Aider in your project with these lines.', de: 'Starte Aider in deinem Projekt mit diesen Zeilen.' }],
    file: {
      name: 'Terminal', language: 'bash',
      template: 'export OPENAI_API_BASE={{baseUrl}}\nexport OPENAI_API_KEY={{apiKey}}\naider --model openai/{{model}}\n',
    },
  },
  {
    id: 'claude-code', name: 'Claude Code', group: 'coding', api: 'anthropic',
    summary: { en: 'Coding agent in the terminal (Anthropic API).', de: 'Coding-Agent im Terminal (Anthropic-API).' },
    steps: [{ en: 'Start Claude Code with these settings.', de: 'Starte Claude Code mit diesen Einstellungen.' }],
    file: {
      name: 'Terminal', language: 'bash',
      template: 'export ANTHROPIC_BASE_URL={{rootUrl}}\nexport ANTHROPIC_AUTH_TOKEN={{apiKey}}\nexport ANTHROPIC_MODEL={{model}}\nclaude\n',
    },
  },
  {
    id: 'cursor', name: 'Cursor', group: 'coding', api: 'openai', runs: 'internet', cloudOnly: true,
    summary: { en: 'AI code editor.', de: 'KI-Code-Editor.' },
    steps: [
      { en: 'Open Cursor Settings → Models.', de: 'Öffne Cursor Settings → Models.' },
      { en: 'Under "API Keys", turn on "OpenAI API Key" and paste the key, then turn on "Override OpenAI Base URL" and paste the address.', de: 'Unter „API Keys“: „OpenAI API Key“ einschalten und den Key einfügen, dann „Override OpenAI Base URL“ einschalten und die Adresse einfügen.' },
      { en: 'Add a custom model named "{{model}}" and turn it on.', de: 'Füge ein eigenes Modell namens „{{model}}“ hinzu und schalte es ein.' },
    ],
    fields: [{ label: 'Override OpenAI Base URL', value: 'baseUrl' }, { label: 'OpenAI API Key', value: 'apiKey' }, { label: 'Model', value: 'model' }],
  },
  {
    id: 'open-webui', name: 'Open WebUI', group: 'chat', api: 'openai', runs: 'docker',
    summary: { en: 'Chat interface in the browser.', de: 'Chat-Oberfläche im Browser.' },
    steps: [
      { en: 'Open Admin Panel → Settings → Connections.', de: 'Öffne Admin Panel → Settings → Connections.' },
      { en: 'Add an OpenAI API connection with the address and key below.', de: 'Füge eine „OpenAI API“-Verbindung mit Adresse und Key unten hinzu.' },
      { en: 'NeoRouter’s models then show up in the model picker; "{{model}}" lets NeoRouter choose.', de: 'NeoRouters Modelle erscheinen dann in der Modellauswahl; mit „{{model}}“ wählt NeoRouter.' },
    ],
    fields: [{ label: 'URL', value: 'baseUrl' }, { label: 'Key', value: 'apiKey' }],
  },
  {
    id: 'librechat', name: 'LibreChat', group: 'chat', api: 'openai', runs: 'docker',
    summary: { en: 'Self-hosted chat interface.', de: 'Selbst gehostete Chat-Oberfläche.' },
    steps: [
      { en: 'Add this endpoint to librechat.yaml under "endpoints" → "custom".', de: 'Füge diesen Endpunkt in der librechat.yaml unter „endpoints“ → „custom“ hinzu.' },
      { en: 'Restart LibreChat.', de: 'Starte LibreChat neu.' },
    ],
    file: {
      name: 'librechat.yaml', language: 'yaml',
      template: 'endpoints:\n  custom:\n    - name: NeoRouter\n      apiKey: {{apiKey}}\n      baseURL: {{baseUrl}}\n      models:\n        default: ["{{model}}"]\n        fetch: true\n      titleConvo: true\n      titleModel: {{model}}\n',
    },
  },
  {
    id: 'n8n', name: 'n8n', group: 'automation', api: 'openai', runs: 'docker',
    summary: { en: 'Workflow automation.', de: 'Workflow-Automatisierung.' },
    steps: [
      { en: 'Create a new "OpenAI" credential.', de: 'Lege einen neuen „OpenAI“-Zugang (Credential) an.' },
      { en: 'Paste the key and the address below and save.', de: 'Füge Key und Adresse unten ein und speichere.' },
      { en: 'In OpenAI nodes, type "{{model}}" as the model.', de: 'Gib in OpenAI-Knoten „{{model}}“ als Modell ein.' },
    ],
    fields: [{ label: 'API Key', value: 'apiKey' }, { label: 'Base URL', value: 'baseUrl' }, { label: 'Model', value: 'model' }],
  },
  {
    id: 'openai-python', name: 'OpenAI SDK (Python)', group: 'developer', api: 'openai',
    summary: { en: 'Your own code in Python.', de: 'Eigener Code in Python.' },
    steps: [{ en: 'Use this in your code.', de: 'Nutze das in deinem Code.' }],
    file: {
      name: 'Python', language: 'python',
      template: 'from openai import OpenAI\n\nclient = OpenAI(base_url="{{baseUrl}}", api_key="{{apiKey}}")\nanswer = client.chat.completions.create(\n    model="{{model}}",\n    messages=[{"role": "user", "content": "Hi"}],\n)\nprint(answer.choices[0].message.content)\n',
    },
  },
  {
    id: 'openai-js', name: 'OpenAI SDK (JavaScript)', group: 'developer', api: 'openai',
    summary: { en: 'Your own code in JavaScript or TypeScript.', de: 'Eigener Code in JavaScript oder TypeScript.' },
    steps: [{ en: 'Use this in your code.', de: 'Nutze das in deinem Code.' }],
    file: {
      name: 'JavaScript', language: 'javascript',
      template: 'import OpenAI from "openai";\n\nconst client = new OpenAI({ baseURL: "{{baseUrl}}", apiKey: "{{apiKey}}" });\nconst answer = await client.chat.completions.create({\n  model: "{{model}}",\n  messages: [{ role: "user", content: "Hi" }],\n});\nconsole.log(answer.choices[0].message.content);\n',
    },
  },
  {
    id: 'curl', name: 'curl', group: 'developer', api: 'openai',
    summary: { en: 'Try it from a terminal.', de: 'Im Terminal ausprobieren.' },
    steps: [{ en: 'Run this in a terminal.', de: 'Führe das in einem Terminal aus.' }],
    file: {
      name: 'Terminal', language: 'bash',
      template: 'curl {{baseUrl}}/chat/completions \\\n  -H "Authorization: Bearer {{apiKey}}" \\\n  -H "Content-Type: application/json" \\\n  -d \'{"model": "{{model}}", "messages": [{"role": "user", "content": "Hi"}]}\'\n',
    },
  },
  {
    id: 'other-openai', name: 'Other app (OpenAI-compatible)', group: 'developer', api: 'openai',
    summary: { en: 'Any app with an "OpenAI compatible" option.', de: 'Jede App mit einer Option „OpenAI compatible“.' },
    steps: [
      { en: 'In the app, choose "OpenAI compatible" (sometimes "Custom OpenAI" or "OpenAI API").', de: 'Wähle in der App „OpenAI compatible“ (manchmal „Custom OpenAI“ oder „OpenAI API“).' },
      { en: 'Fill in the fields below.', de: 'Trage die Felder unten ein.' },
    ],
    fields: OPENAI_FIELDS,
  },
  {
    id: 'other-anthropic', name: 'Other app (Anthropic API)', group: 'developer', api: 'anthropic',
    summary: { en: 'Any app that talks to Claude’s API.', de: 'Jede App, die Claudes API nutzt.' },
    steps: [{ en: 'Set the app’s Anthropic base URL and key to these values.', de: 'Setze Basis-URL und Key für Anthropic in der App auf diese Werte.' }],
    fields: [{ label: 'Base URL', value: 'rootUrl' }, { label: 'API Key', value: 'apiKey' }, { label: 'Model', value: 'model' }],
  },
].map((entry) => app.parse(entry));

const byId = new Map(APPS.map((entry) => [entry.id, entry]));

module.exports = { all: () => APPS, get: (id) => byId.get(id) || null };
