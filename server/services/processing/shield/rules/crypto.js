'use strict';

// Cryptocurrency wallet addresses. Seed phrases are secrets (see
// detectors/seed_phrase.js).
const WALLET_WORDS = ['eth', 'ether', 'ethereum', 'wallet', 'address', 'adresse', 'wallet-adresse', 'metamask', 'send', 'senden', 'usdt', 'usdc', 'erc20', 'erc-20', 'polygon', 'bsc', 'bnb', 'arbitrum', 'token', 'krypto', 'crypto'];

module.exports = [
  {
    id: 'btc_base58',
    label: { en: 'Bitcoin / Litecoin / Tron address', de: 'Bitcoin-, Litecoin- oder Tron-Adresse' },
    category: 'crypto',
    pattern: /\b(?:[13][a-km-zA-HJ-NP-Z1-9]{25,34}|[LM][a-km-zA-HJ-NP-Z1-9]{26,33}|T[a-km-zA-HJ-NP-Z1-9]{33})\b/g,
    validator: 'base58Check',
    score: 0.4,
    tests: { match: ['send to 1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa'], ignore: ['1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNb'] },
  },
  {
    id: 'bech32_address',
    label: { en: 'Bitcoin / Litecoin address', de: 'Bitcoin- oder Litecoin-Adresse' },
    category: 'crypto',
    pattern: /\b(?:bc|tb|ltc|tltc)1[02-9ac-hj-np-z]{11,87}\b/gi,
    validator: 'bech32',
    score: 0.4,
    tests: { match: ['bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq'], ignore: ['bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdx'] },
  },
  {
    id: 'eth_address',
    label: { en: 'Ethereum address', de: 'Ethereum-Adresse' },
    category: 'crypto',
    pattern: /\b0x[a-fA-F0-9]{40}\b/g,
    score: 0.4,
    context: { positive: WALLET_WORDS, window: 40 },
    tests: { match: ['wallet 0x742d35cc6634c0532925a3b844bc454e4438f44e'], ignore: ['hash 0x742d35cc6634c0532925a3b844bc454e4438f44e'] },
  },
  {
    id: 'eth_address_checksummed',
    label: { en: 'Ethereum address', de: 'Ethereum-Adresse' },
    category: 'crypto',
    pattern: /\b0x[a-fA-F0-9]{40}\b/g,
    accept: 'mixedCaseHex',
    score: 0.6,
    tests: { match: ['0x742d35Cc6634C0532925a3b844Bc454e4438f44e'], ignore: ['0x742d35cc6634c0532925a3b844bc454e4438f44e'] },
  },
  {
    id: 'monero_address',
    label: { en: 'Monero address', de: 'Monero-Adresse' },
    category: 'crypto',
    pattern: /\b[48][0-9AB][1-9A-HJ-NP-Za-km-z]{93}\b/g,
    score: 0.6,
    tests: { match: ['44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A'] },
  },
  {
    id: 'solana_address',
    label: { en: 'Solana address', de: 'Solana-Adresse' },
    category: 'crypto',
    pattern: /\b[1-9A-HJ-NP-Za-km-z]{32,44}\b/g,
    accept: 'mixedAlnum',
    score: 0.2,
    context: { positive: ['solana', 'sol', 'phantom'], window: 40 },
    tests: { match: ['Solana wallet 7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV'], ignore: ['7EcDhSYGxXyscszYEp35KHN8vvw3svAuLKTzXwCFLtV'] },
  },
];
