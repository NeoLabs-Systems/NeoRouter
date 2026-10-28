'use strict';

function createGitHelpers(run) {
  if (typeof run !== 'function') {
    throw new TypeError('createGitHelpers(run) requires a run function');
  }

  function listGitTagVersions(pattern) {
    const res = run('git', ['tag', '--list', pattern, '--sort=-v:refname']);
    if (res.status !== 0) return [];
    return String(res.stdout || '')
      .split('\n')
      .map((value) => value.trim().replace(/^v/, ''))
      .filter(Boolean);
  }

  function latestGitTagVersion(pattern) {
    return listGitTagVersions(pattern)[0] || null;
  }

  function latestStableGitTagVersion() {
    return listGitTagVersions('v[0-9]*.[0-9]*.[0-9]*')
      .find((version) => /^\d+\.\d+\.\d+$/.test(version)) || null;
  }

  function latestBetaGitTagVersion() {
    return listGitTagVersions('v[0-9]*.[0-9]*.[0-9]*-beta.*')
      .find((version) => /^\d+\.\d+\.\d+-beta\.\d+$/.test(version)) || null;
  }

  function gitWorkingTreeDirty() {
    const res = run('git', ['status', '--porcelain']);
    return res.status === 0 && Boolean(String(res.stdout || '').trim());
  }

  function gitStashPush(message) {
    const label = String(message || '').trim();
    if (!label) {
      throw new Error('A stash message is required.');
    }
    const res = run('git', ['stash', 'push', '--include-untracked', '--message', label]);
    if (res.status !== 0) {
      const detail = String(res.stderr || res.stdout || '').trim();
      throw new Error(detail || 'git stash push failed');
    }
    const output = `${res.stdout || ''}\n${res.stderr || ''}`;
    if (/No local changes to save/i.test(output)) return null;
    return label;
  }

  function gitStashPopByMessage(message) {
    const label = String(message || '').trim();
    if (!label) return { restored: false, missing: true };
    const list = run('git', ['stash', 'list']);
    if (list.status !== 0) {
      return {
        restored: false,
        missing: false,
        error: String(list.stderr || list.stdout || '').trim() || 'git stash list failed',
      };
    }
    const lines = String(list.stdout || '')
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean);
    const index = lines.findIndex((line) => line.includes(label));
    if (index < 0) return { restored: false, missing: true };
    const popped = run('git', ['stash', 'pop', `stash@{${index}}`]);
    if (popped.status !== 0) {
      return {
        restored: false,
        missing: false,
        error: String(popped.stderr || popped.stdout || '').trim() || 'git stash pop failed',
      };
    }
    return { restored: true, missing: false };
  }

  function gitLocalBranchExists(branch) {
    return run('git', ['show-ref', '--verify', '--quiet', `refs/heads/${branch}`]).status === 0;
  }

  function gitRemoteBranchExists(branch) {
    return run('git', ['ls-remote', '--exit-code', '--heads', 'origin', branch]).status === 0;
  }

  return {
    listGitTagVersions,
    latestGitTagVersion,
    latestStableGitTagVersion,
    latestBetaGitTagVersion,
    gitWorkingTreeDirty,
    gitStashPush,
    gitStashPopByMessage,
    gitLocalBranchExists,
    gitRemoteBranchExists,
  };
}

module.exports = { createGitHelpers };
