'use strict';

// Claude through the official Claude Code CLI with the user's own Pro/Max
// plan. Each connection holds that user's token from `claude setup-token`
// (stored encrypted) and has its own Claude Code configuration folder
// (CLAUDE_CONFIG_DIR), so connections never see each other's login. The token
// is only ever handed to the unmodified `claude` binary, which NeoRouter runs
// as `claude -p` and reads as stream-json. Every run gets a clean environment
// (no server-wide Anthropic credentials can take over), no tools, MCP servers,
// slash commands or saved sessions, and an empty working folder. The binary is
// a server setting (NEOROUTER_CLAUDE_PATH), never something a user enters.
const { spawn, execFile } = require('node:child_process');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { BaseAdapter } = require('./base_adapter');
const { normalizeModel } = require('../model_info');
const { ProviderError, KINDS, classifyHttpError } = require('../provider_error');
const { completionId, buildChunk, buildResponse, textOf, StreamAccumulator } = require('../../capabilities/chat/canonical');

const { RUNTIME_HOME } = require('../../../runtime/paths');

const CANDIDATES = [process.env.NEOROUTER_CLAUDE_PATH, 'claude', path.join(os.homedir(), '.claude', 'local', 'claude'), path.join(os.homedir(), '.local', 'bin', 'claude'), '/opt/homebrew/bin/claude', '/usr/local/bin/claude'];
const EFFORTS = new Set(['low', 'medium', 'high', 'xhigh', 'max']);
const AUTH_TIMEOUT_MS = 10_000;

// Credentials and provider switches Claude Code would rank above the
// connection's own token (code.claude.com/docs/en/authentication).
const FOREIGN_AUTH = /^(ANTHROPIC_|CLAUDE_CODE_USE_|CLAUDE_CODE_OAUTH_TOKEN$|CLAUDE_CONFIG_DIR$|AWS_BEARER_TOKEN_BEDROCK$)/;

function executable() {
  return CANDIDATES.find((candidate) => candidate && (candidate === 'claude' || fs.existsSync(candidate))) || 'claude';
}

// The connection's private Claude Code folder (settings, the Keychain entry
// on macOS, session state), created owner-only.
function configDir(accountId) {
  const dir = accountId ? path.join(RUNTIME_HOME, 'claude', accountId) : fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-claude-check-'));
  fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  return dir;
}

function environment(token, accountId) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([name]) => !FOREIGN_AUTH.test(name)));
  return { ...env, CLAUDE_CONFIG_DIR: configDir(accountId), CLAUDE_CODE_OAUTH_TOKEN: token || '', CLAUDE_CODE_SKIP_PROMPT_HISTORY: '1' };
}

// The conversation as one prompt: the CLI takes a single user turn, so
// earlier turns are given as a transcript.
function toPrompt(messages) {
  const turns = messages.filter((message) => message.role === 'user' || message.role === 'assistant');
  const last = turns[turns.length - 1];
  const history = turns.slice(0, -1).map((message) => `${message.role === 'user' ? 'User' : 'Assistant'}: ${textOf(message.content)}`);
  const current = last ? textOf(last.content) : '';
  return history.length ? `Conversation so far:\n\n${history.join('\n\n')}\n\nUser: ${current}` : current;
}

function systemPrompt(messages) {
  const text = messages.filter((message) => message.role === 'system' || message.role === 'developer').map((message) => textOf(message.content)).join('\n\n');
  return text || 'You are a helpful assistant.';
}

// "Claude AI usage limit reached|1793000000" or similar: a used-up plan window.
function limitError(text) {
  const reset = Number(/\|(\d{9,})/.exec(text)?.[1]);
  const headers = new Headers();
  if (Number.isFinite(reset)) headers.set('retry-after', String(Math.max(60, Math.round(reset - Date.now() / 1000))));
  return classifyHttpError(429, { error: { message: text.replace(/\|\d+$/, '') } }, headers);
}

class ClaudeCliAdapter extends BaseAdapter {
  run(args, { input = '', signal, timeoutMs }) {
    const workdir = fs.mkdtempSync(path.join(os.tmpdir(), 'neorouter-claude-'));
    const child = spawn(executable(), args, { cwd: workdir, env: environment(this.secret, this.account.id), stdio: ['pipe', 'pipe', 'pipe'] });
    const stop = () => child.kill('SIGINT');
    signal?.addEventListener('abort', stop, { once: true });
    const timer = timeoutMs ? setTimeout(stop, timeoutMs) : null;
    child.on('close', () => {
      clearTimeout(timer);
      signal?.removeEventListener('abort', stop);
      fs.rm(workdir, { recursive: true, force: true }, () => {});
    });
    child.stdin.on('error', () => {});
    child.stdin.end(input);
    return child;
  }

  status() {
    return new Promise((resolve) => {
      const env = environment(this.secret, this.account.id);
      execFile(executable(), ['auth', 'status'], { timeout: AUTH_TIMEOUT_MS, env }, (error, stdout) => {
        if (!this.account.id) fs.rm(env.CLAUDE_CONFIG_DIR, { recursive: true, force: true }, () => {});
        if (error?.code === 'ENOENT') return resolve({ installed: false });
        let info = null;
        try { info = JSON.parse(stdout); } catch { info = null; }
        // Exit code 0 means signed in, 1 means not.
        resolve({ installed: true, loggedIn: !error && info?.loggedIn !== false, authMethod: info?.authMethod || null, timedOut: error?.killed === true });
      });
    });
  }

  async validate() {
    const state = await this.status();
    if (!this.secret) return { status: 'invalid', message: 'Paste the token from `claude setup-token`.' };
    if (!state.installed) return { status: 'unknown', message: 'Claude Code is not installed on the NeoRouter server yet; the administrator installs it once.' };
    if (state.timedOut) return { status: 'unknown', message: 'Claude Code did not answer in time.' };
    if (!state.loggedIn) return { status: 'invalid', message: 'Claude Code did not accept this token. Create a new one with `claude setup-token`.' };
    return { status: 'healthy', message: null };
  }

  async listModels() {
    return (this.definition.models?.static || []).map((entry) => normalizeModel(entry, { tools: false, vision: false, reasoning: true }));
  }

  async *stream(canonical, { modelId, signal, timeoutMs, onHeaders }) {
    const args = ['-p', '--output-format', 'stream-json', '--verbose', '--include-partial-messages', '--model', modelId,
      '--system-prompt', systemPrompt(canonical.messages), '--tools', '', '--strict-mcp-config', '--disable-slash-commands',
      '--no-session-persistence', '--max-turns', '1'];
    if (EFFORTS.has(canonical.reasoning_effort)) args.push('--effort', canonical.reasoning_effort);
    const child = this.run(args, { input: toPrompt(canonical.messages), signal, timeoutMs: timeoutMs || this.timeoutMs });
    onHeaders?.(new Headers());
    let stderr = '';
    child.stderr.on('data', (chunk) => { stderr = (stderr + chunk).slice(-2000); });
    const exited = new Promise((resolve) => child.on('close', resolve));
    const id = completionId();
    const created = Math.floor(Date.now() / 1000);
    let buffer = '';
    let streamed = false;
    let result = null;
    yield buildChunk({ id, model: modelId, created, delta: { role: 'assistant', content: '' } });
    for await (const chunk of child.stdout) {
      buffer += chunk;
      let index;
      while ((index = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, index).trim();
        buffer = buffer.slice(index + 1);
        if (!line) continue;
        let event;
        try { event = JSON.parse(line); } catch { continue; }
        if (event.type === 'stream_event' && event.event?.delta?.type === 'text_delta' && event.event.delta.text) {
          streamed = true;
          yield buildChunk({ id, model: modelId, created, delta: { content: event.event.delta.text } });
        } else if (event.type === 'stream_event' && event.event?.delta?.type === 'thinking_delta' && event.event.delta.thinking) {
          yield buildChunk({ id, model: modelId, created, delta: { reasoning_content: event.event.delta.thinking } });
        } else if (event.type === 'result') {
          result = event;
        }
      }
    }
    const code = await exited;
    if (signal?.aborted) throw new ProviderError(KINDS.ABORTED, 'The request was aborted.');
    if (!result) {
      if (/not (logged|signed) in|login|authenticat/i.test(stderr)) throw new ProviderError(KINDS.AUTH, 'Claude Code is not signed in on the NeoRouter server.');
      throw new ProviderError(KINDS.SERVER, `Claude Code exited (${code}) without an answer.${stderr ? ` ${stderr.trim().split('\n').pop()}` : ''}`);
    }
    if (result.is_error || (result.subtype && result.subtype !== 'success')) {
      const text = String(result.result || result.subtype || 'Claude Code failed.');
      if (/limit|usage|quota/i.test(text)) throw limitError(text);
      if (/log ?in|authenticat|credential/i.test(text)) throw new ProviderError(KINDS.AUTH, text);
      throw new ProviderError(KINDS.SERVER, text);
    }
    // Older versions without partial messages: the whole answer comes at the end.
    if (!streamed && result.result) yield buildChunk({ id, model: modelId, created, delta: { content: String(result.result) } });
    const usage = result.usage ? { prompt_tokens: (result.usage.input_tokens || 0) + (result.usage.cache_read_input_tokens || 0) + (result.usage.cache_creation_input_tokens || 0), completion_tokens: result.usage.output_tokens || 0 } : null;
    if (usage) usage.total_tokens = usage.prompt_tokens + usage.completion_tokens;
    yield buildChunk({ id, model: modelId, created, delta: {}, finishReason: 'stop', usage });
  }

  async chat(canonical, context) {
    const accumulator = new StreamAccumulator();
    for await (const piece of this.stream(canonical, context)) accumulator.push(piece);
    if (!accumulator.hasAnswer()) throw new ProviderError(KINDS.EMPTY, 'Claude Code returned an empty answer.');
    return { response: buildResponse({ model: context.modelId, message: accumulator.toMessage(), finishReason: 'stop', usage: accumulator.usage }), headers: new Headers() };
  }
}

module.exports = { ClaudeCliAdapter, toPrompt, systemPrompt, limitError };
