'use strict';

// WAV helpers for speech. Some providers cap the text of one request (Groq's
// Orpheus), so a longer text is spoken in parts whose WAV files are joined
// into one. Others answer raw PCM, which is wrapped into a WAV file so that
// players and the gateway's converter can read it.
const { ProviderError, KINDS } = require('../provider_error');

function wavHeader({ format = 1, channels, sampleRate, bitsPerSample }, dataLength) {
  const blockAlign = channels * (bitsPerSample / 8);
  const header = Buffer.alloc(44);
  header.write('RIFF', 0, 'ascii');
  header.writeUInt32LE(36 + dataLength, 4);
  header.write('WAVE', 8, 'ascii');
  header.write('fmt ', 12, 'ascii');
  header.writeUInt32LE(16, 16);
  header.writeUInt16LE(format, 20);
  header.writeUInt16LE(channels, 22);
  header.writeUInt32LE(sampleRate, 24);
  header.writeUInt32LE(sampleRate * blockAlign, 28);
  header.writeUInt16LE(blockAlign, 32);
  header.writeUInt16LE(bitsPerSample, 34);
  header.write('data', 36, 'ascii');
  header.writeUInt32LE(dataLength, 40);
  return header;
}

/** { fmt, data } of a WAV file, or null when the bytes are not one. */
function parseWav(buffer) {
  if (buffer.length < 12 || buffer.toString('ascii', 0, 4) !== 'RIFF' || buffer.toString('ascii', 8, 12) !== 'WAVE') return null;
  let fmt = null;
  let offset = 12;
  while (offset + 8 <= buffer.length) {
    const id = buffer.toString('ascii', offset, offset + 4);
    const size = buffer.readUInt32LE(offset + 4);
    const start = offset + 8;
    if (id === 'fmt ' && start + 16 <= buffer.length) {
      fmt = { format: buffer.readUInt16LE(start), channels: buffer.readUInt16LE(start + 2), sampleRate: buffer.readUInt32LE(start + 4), bitsPerSample: buffer.readUInt16LE(start + 14) };
    }
    if (id === 'data') {
      if (!fmt) return null;
      // Streamed WAVs leave the data size unset (0 or 0xFFFFFFFF): the samples run to the end.
      const end = size && start + size <= buffer.length ? start + size : buffer.length;
      return { fmt, data: buffer.subarray(start, end) };
    }
    // Chunks are padded to an even length.
    offset = start + size + (size % 2);
  }
  return null;
}

/** One WAV file from several with the same sample format, samples in order. */
function joinWav(buffers) {
  const parts = buffers.map(parseWav);
  if (parts.some((part) => !part)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned audio that is not WAV, so its parts cannot be joined.');
  const [first] = parts;
  const sameFormat = (part) => ['format', 'channels', 'sampleRate', 'bitsPerSample'].every((field) => part.fmt[field] === first.fmt[field]);
  if (!parts.every(sameFormat)) throw new ProviderError(KINDS.INVALID_OUTPUT, 'The provider returned audio parts in different formats.');
  const data = Buffer.concat(parts.map((part) => part.data));
  return Buffer.concat([wavHeader(first.fmt, data.length), data]);
}

/** 16-bit mono PCM wrapped into a WAV file. */
function pcmToWav(pcm, sampleRate) {
  return Buffer.concat([wavHeader({ channels: 1, sampleRate, bitsPerSample: 16 }, pcm.length), pcm]);
}

module.exports = { parseWav, joinWav, pcmToWav, wavHeader };
