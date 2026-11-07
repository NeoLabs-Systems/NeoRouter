import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// The one explanation of what the pool means for privacy, shown on the pool
/// tab, when joining, and behind the info button next to a key's pool switch.
Future<void> showPoolTrustInfo(BuildContext context) {
  final t = AppL10n.of(context);
  final palette = neoRouterPaletteOf(context);
  return showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(t.teamTabPool),
      content: SizedBox(
        width: 440,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Text(t.poolDescription, style: TextStyle(color: palette.textSoft, height: 1.45)),
            const SizedBox(height: AppSpacing.md),
            InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined),
          ],
        ),
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.close))],
    ),
  );
}

String poolStateLabel(AppL10n t, String state) => switch (state) {
      'online' => t.poolStateOnline,
      'offline' => t.poolStateOffline,
      'paused' => t.poolStatePaused,
      _ => t.poolStateConnecting,
    };

PillTone poolStateTone(String state) => switch (state) {
      'online' => PillTone.success,
      'offline' => PillTone.warning,
      _ => PillTone.neutral,
    };

String _balanceLabel(AppL10n t, String balance) => switch (balance) {
      'balanced' => t.poolBalanceBalanced,
      'giving_more' => t.poolBalanceGiving,
      'taking_more' => t.poolBalanceTaking,
      _ => t.poolBalanceNew,
    };

/// The quick switch: the whole pool on or off. The same control on the home
/// page and on the pool tab, so it reads the same everywhere.
class PoolSwitchRow extends StatefulWidget {
  const PoolSwitchRow({super.key, required this.controller, this.description});

  final NeoRouterController controller;
  final String? description;

  @override
  State<PoolSwitchRow> createState() => _PoolSwitchRowState();
}

class _PoolSwitchRowState extends State<PoolSwitchRow> {
  bool _busy = false;

  Future<void> _toggle(bool on) async {
    setState(() => _busy = true);
    try {
      await widget.controller.setPoolPaused(!on);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final pool = widget.controller.pool;
    final paused = pool?.paused ?? false;
    return SettingsRow(
      label: t.poolSwitchLabel,
      description: widget.description ?? (paused ? t.homePoolPaused : t.poolSwitchHint),
      control: Switch(value: !paused, onChanged: _busy || !widget.controller.canManagePool ? null : _toggle),
    );
  }
}

/// The pool tab of the Team page.
class PoolTab extends StatefulWidget {
  const PoolTab({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<PoolTab> createState() => _PoolTabState();
}

class _PoolTabState extends State<PoolTab> {
  bool _loading = true;
  String? _error;

  NeoRouterController get controller => widget.controller;
  PoolSummary? get pool => controller.pool;

  @override
  void initState() {
    super.initState();
    controller.addListener(_changed);
    _load().then((_) {
      final link = controller.pendingPoolInvite;
      if (link != null && mounted && !(pool?.inPool ?? false)) {
        controller.consumePoolInvite();
        _join(link);
      }
    });
  }

  @override
  void dispose() {
    controller.removeListener(_changed);
    super.dispose();
  }

  void _changed() {
    if (mounted) setState(() {});
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      await controller.refreshPool();
      if (controller.pool == null) _error = 'The pool could not be loaded.';
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _apply(Future<PoolSummary> Function() action) async {
    try {
      controller.updatePool(await action());
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  Future<void> _create() async {
    final created = await showDialog<PoolSummary>(context: context, builder: (_) => _CreatePoolDialog(controller: controller));
    if (created != null) controller.updatePool(created);
  }

  Future<void> _join([String? link]) async {
    final joined = await showDialog<PoolSummary>(context: context, builder: (_) => _JoinPoolDialog(controller: controller, initialLink: link));
    if (joined != null) controller.updatePool(joined);
  }

  Future<void> _invite() async {
    final t = AppL10n.of(context);
    try {
      final link = await controller.api.createPoolInvite();
      await controller.refreshPool();
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text(t.poolInviteTitle),
          content: SizedBox(width: 480, child: _CopyableLink(intro: t.poolInviteBody, link: link)),
          actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.keySecretDone))],
        ),
      );
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  Future<void> _leave(PoolSummary current) async {
    final t = AppL10n.of(context);
    final closing = current.isHost && current.partners.isNotEmpty;
    final confirmed = await confirmAction(
      context,
      title: closing ? t.poolClose : t.poolLeave,
      message: closing ? t.poolCloseConfirm(current.name) : t.poolLeaveConfirm(current.name),
      confirmLabel: closing ? t.poolClose : t.poolLeave,
      cancelLabel: t.cancel,
    );
    if (confirmed) await _apply(controller.api.leavePool);
  }

  Future<void> _remove(PoolPartner partner) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.poolRemoveMember, message: t.poolRemoveConfirm(partner.name), confirmLabel: t.poolRemoveMember, cancelLabel: t.cancel);
    if (confirmed) await _apply(() => controller.api.removePoolMember(partner.memberId));
  }

  Future<void> _editLimits(PoolShare share) async {
    final next = await showDialog<Map<String, Object?>>(context: context, builder: (_) => _LimitsDialog(share: share));
    if (next != null) await _apply(() => controller.api.updatePoolShare(next));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final current = pool;
    return AsyncBody(
      loading: _loading,
      error: _error,
      onRetry: _load,
      child: current == null
          ? const SizedBox()
          : current.inPool
              ? _inPool(t, current)
              : EmptyState(
                  icon: Icons.hub_outlined,
                  title: t.poolEmptyTitle,
                  message: t.poolEmptyMessage,
                  action: controller.canManagePool
                      ? Wrap(
                          spacing: 8,
                          runSpacing: 8,
                          alignment: WrapAlignment.center,
                          children: <Widget>[
                            FilledButton.icon(onPressed: _create, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.poolCreate)),
                            OutlinedButton.icon(onPressed: _join, icon: const Icon(Icons.link_rounded, size: 18), label: Text(t.poolJoin)),
                          ],
                        )
                      : null,
                ),
    );
  }

  Widget _inPool(AppL10n t, PoolSummary current) {
    final palette = neoRouterPaletteOf(context);
    final manage = controller.canManagePool;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsGroup(
          title: current.name,
          children: <Widget>[
            PoolSwitchRow(controller: controller),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
              child: InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined),
            ),
          ],
        ),
        SettingsGroup(
          title: t.poolPartnersTitle,
          description: t.poolPartnersHint,
          trailing: manage && current.isHost
              ? OutlinedButton.icon(onPressed: _invite, icon: const Icon(Icons.person_add_alt_1_outlined, size: 18), label: Text(t.poolInvite))
              : null,
          children: current.partners.isEmpty
              ? <Widget>[Padding(padding: const EdgeInsets.all(20), child: Text(t.poolNoPartners, style: TextStyle(color: palette.textSoft)))]
              : <Widget>[for (final partner in current.partners) _partnerRow(t, palette, current, partner)],
        ),
        if (manage && current.invites.isNotEmpty)
          SettingsGroup(
            title: t.inviteLinksTitle,
            children: <Widget>[
              for (final invite in current.invites)
                ListRow(
                  leading: Icon(Icons.link_rounded, size: 18, color: palette.textSoft),
                  title: t.poolInviteTitle,
                  subtitle: t.inviteExpires(formatTime(DateTime.parse(invite['expiresAt'] as String).toLocal())),
                  trailing: TextButton(
                    onPressed: () async {
                      try {
                        await controller.api.revokePoolInvite(invite['id'] as String);
                        await controller.refreshPool();
                      } on ApiException catch (error) {
                        if (mounted) showSnack(context, error.message);
                      }
                    },
                    child: Text(t.revoke, style: TextStyle(color: palette.error)),
                  ),
                ),
            ],
          ),
        if (manage) _shareGroup(t, palette, current),
        if (manage)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: () => _leave(current),
              child: Text(current.isHost && current.partners.isNotEmpty ? t.poolClose : t.poolLeave, style: TextStyle(color: palette.error)),
            ),
          ),
      ],
    );
  }

  Widget _partnerRow(AppL10n t, NeoRouterPalette palette, PoolSummary current, PoolPartner partner) {
    final today = partner.today;
    final subtitle = <String>[
      if (partner.isHost) t.poolHost,
      t.poolPartnerModels('${partner.models}'),
      t.poolToday(formatCount(today.givenRequests), formatCount(today.takenRequests)),
    ].join(' · ');
    final linkId = partner.linkId;
    final canPause = controller.canManagePool && linkId != null;
    final canRemove = controller.canManagePool && current.isHost;
    return ListRow(
      leading: ProviderAvatar(label: partner.name, size: 32),
      title: partner.name,
      subtitle: partner.problem != null && partner.state == 'offline' ? '$subtitle · ${partner.problem}' : subtitle,
      trailing: Row(
        mainAxisSize: MainAxisSize.min,
        children: <Widget>[
          if (partner.balance != 'new') ...<Widget>[MetaPill(label: _balanceLabel(t, partner.balance)), const SizedBox(width: 8)],
          StatusLabel(label: poolStateLabel(t, partner.state), tone: poolStateTone(partner.state)),
          if (canPause || canRemove)
            PopupMenuButton<String>(
              tooltip: t.moreActions,
              icon: const Icon(Icons.more_horiz_rounded, size: 20),
              onSelected: (value) => value == 'remove'
                  ? _remove(partner)
                  : _apply(() => controller.api.setPartnerPaused(linkId!, partner.state != 'paused')),
              itemBuilder: (_) => <PopupMenuEntry<String>>[
                if (canPause) PopupMenuItem<String>(value: 'pause', child: Text(partner.state == 'paused' ? t.poolResumePartner : t.poolPausePartner)),
                if (canRemove) PopupMenuItem<String>(value: 'remove', child: Text(t.poolRemoveMember, style: TextStyle(color: palette.error))),
              ],
            ),
        ],
      ),
    );
  }

  Widget _shareGroup(AppL10n t, NeoRouterPalette palette, PoolSummary current) {
    final share = current.share;
    return SettingsGroup(
      title: t.poolShareTitle,
      description: t.poolShareHint,
      children: <Widget>[
        if (current.shareable.isEmpty)
          Padding(padding: const EdgeInsets.all(20), child: Text(t.poolShareNone, style: TextStyle(color: palette.textSoft)))
        else
          for (final account in current.shareable)
            SettingsRow(
              label: account.label,
              description: t.poolFreeModels('${account.freeModels}'),
              control: Switch(value: account.shared, onChanged: (on) => _apply(() => controller.api.setAccountPoolShare(account.id, on))),
            ),
        SettingsRow(
          label: t.poolLimitsLabel,
          description: t.poolLimitsValue(formatCount(share.requestsPerDay), formatCount(share.tokensPerDay), '${share.concurrent}', '${(share.reserve * 100).round()}'),
          control: OutlinedButton(onPressed: () => _editLimits(share), child: Text(t.poolLimitsChange)),
        ),
        Padding(padding: const EdgeInsets.fromLTRB(20, 0, 20, 16), child: InlineHint(message: t.poolTosNotice)),
      ],
    );
  }
}

class _CopyableLink extends StatelessWidget {
  const _CopyableLink({required this.intro, required this.link});

  final String intro;
  final String link;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(intro, style: TextStyle(color: palette.textSoft, height: 1.45)),
        const SizedBox(height: AppSpacing.md),
        Container(
          padding: const EdgeInsets.fromLTRB(14, 12, 4, 12),
          decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
          child: Row(
            children: <Widget>[
              Expanded(child: SelectableText(link, style: GoogleFonts.geistMono(fontSize: 12.5, color: palette.text))),
              IconButton(
                tooltip: t.copy,
                icon: const Icon(Icons.copy_rounded, size: 17),
                onPressed: () {
                  Clipboard.setData(ClipboardData(text: link));
                  showSnack(context, t.copied);
                },
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Ticks for the connections to share; free ones start ticked.
class _ShareChecklist extends StatelessWidget {
  const _ShareChecklist({required this.accounts, required this.selected, required this.onChanged});

  final List<ShareableAccount> accounts;
  final Set<String> selected;
  final ValueChanged<Set<String>> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (accounts.isEmpty) return Text(t.poolShareNone, style: TextStyle(color: palette.textSoft, fontSize: 13));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(t.poolChooseShared, style: TextStyle(color: palette.textMuted, fontSize: 12.5, fontWeight: FontWeight.w600)),
        for (final account in accounts)
          CheckboxListTile(
            dense: true,
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            value: selected.contains(account.id),
            title: Text(account.label),
            subtitle: Text(t.poolFreeModels('${account.freeModels}')),
            onChanged: (on) => onChanged(on == true ? <String>{...selected, account.id} : (<String>{...selected}..remove(account.id))),
          ),
        const SizedBox(height: 4),
        InlineHint(message: t.poolTosNotice),
      ],
    );
  }
}

class _CreatePoolDialog extends StatefulWidget {
  const _CreatePoolDialog({required this.controller});

  final NeoRouterController controller;

  @override
  State<_CreatePoolDialog> createState() => _CreatePoolDialogState();
}

class _CreatePoolDialogState extends State<_CreatePoolDialog> {
  late final TextEditingController _name = TextEditingController();
  late final TextEditingController _selfName = TextEditingController(text: widget.controller.user?.displayName ?? '');
  late Set<String> _shared = <String>{for (final account in widget.controller.pool?.shareable ?? const <ShareableAccount>[]) account.id};
  bool _understood = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _selfName.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final created = await widget.controller.api.createPool(_name.text.trim(), _selfName.text.trim(), _shared.toList());
      if (mounted) Navigator.of(context).pop(created);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final ready = _name.text.trim().isNotEmpty && _selfName.text.trim().isNotEmpty && _understood && !_busy;
    return AlertDialog(
      title: Text(t.poolCreateTitle),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _name, autofocus: true, decoration: InputDecoration(labelText: t.poolNameLabel), onChanged: (_) => setState(() {})),
              const SizedBox(height: AppSpacing.md),
              TextField(controller: _selfName, decoration: InputDecoration(labelText: t.poolSelfNameLabel, helperText: t.poolSelfNameHint), onChanged: (_) => setState(() {})),
              const SizedBox(height: AppSpacing.lg),
              _ShareChecklist(accounts: widget.controller.pool?.shareable ?? const <ShareableAccount>[], selected: _shared, onChanged: (next) => setState(() => _shared = next)),
              const SizedBox(height: AppSpacing.md),
              _TrustConfirm(value: _understood, onChanged: (value) => setState(() => _understood = value)),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: ready ? _submit : null, child: Text(t.poolCreate)),
      ],
    );
  }
}

/// The privacy notice, confirmed once before creating or joining a pool.
class _TrustConfirm extends StatelessWidget {
  const _TrustConfirm({required this.value, required this.onChanged});

  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined),
        CheckboxListTile(
          dense: true,
          contentPadding: EdgeInsets.zero,
          controlAffinity: ListTileControlAffinity.leading,
          value: value,
          title: Text(t.poolTrustConfirm),
          onChanged: (on) => onChanged(on == true),
        ),
      ],
    );
  }
}

class _JoinPoolDialog extends StatefulWidget {
  const _JoinPoolDialog({required this.controller, this.initialLink});

  final NeoRouterController controller;
  final String? initialLink;

  @override
  State<_JoinPoolDialog> createState() => _JoinPoolDialogState();
}

class _JoinPoolDialogState extends State<_JoinPoolDialog> {
  late final TextEditingController _link = TextEditingController(text: widget.initialLink ?? '');
  late final TextEditingController _selfName = TextEditingController(text: widget.controller.user?.displayName ?? '');
  late Set<String> _shared = <String>{for (final account in widget.controller.pool?.shareable ?? const <ShareableAccount>[]) account.id};
  Map<String, dynamic>? _preview;
  bool _understood = false;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    if (widget.initialLink != null) WidgetsBinding.instance.addPostFrameCallback((_) => _check());
  }

  @override
  void dispose() {
    _link.dispose();
    _selfName.dispose();
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } on ApiException catch (error) {
      if (mounted) setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _check() => _run(() async {
        final preview = await widget.controller.api.previewPool(_link.text.trim());
        if (mounted) setState(() => _preview = preview);
      });

  Future<void> _submit() => _run(() async {
        final joined = await widget.controller.api.joinPool(_link.text.trim(), _selfName.text.trim(), _shared.toList());
        if (mounted) Navigator.of(context).pop(joined);
      });

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final preview = _preview;
    final poolName = (preview?['pool'] as Map?)?['name'] as String? ?? '';
    final hostName = (preview?['host'] as Map?)?['name'] as String? ?? '';
    final members = (preview?['members'] as List?)?.length ?? 0;
    return AlertDialog(
      title: Text(preview == null ? t.poolJoinTitle : t.poolJoinPreview(poolName)),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (preview == null)
                TextField(controller: _link, autofocus: true, decoration: InputDecoration(labelText: t.poolLinkLabel), onChanged: (_) => setState(() {}), onSubmitted: (_) => _check())
              else ...<Widget>[
                Text(t.poolJoinMembers(hostName, '$members'), style: TextStyle(color: palette.textSoft)),
                const SizedBox(height: AppSpacing.md),
                TextField(controller: _selfName, decoration: InputDecoration(labelText: t.poolSelfNameLabel, helperText: t.poolSelfNameHint), onChanged: (_) => setState(() {})),
                const SizedBox(height: AppSpacing.lg),
                _ShareChecklist(accounts: widget.controller.pool?.shareable ?? const <ShareableAccount>[], selected: _shared, onChanged: (next) => setState(() => _shared = next)),
                const SizedBox(height: AppSpacing.md),
                _TrustConfirm(value: _understood, onChanged: (value) => setState(() => _understood = value)),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        if (preview == null)
          FilledButton(onPressed: _busy || _link.text.trim().isEmpty ? null : _check, child: Text(t.poolCheckLink))
        else
          FilledButton(onPressed: _busy || !_understood || _selfName.text.trim().isEmpty ? null : _submit, child: Text(t.poolJoinAction)),
      ],
    );
  }
}

class _LimitsDialog extends StatefulWidget {
  const _LimitsDialog({required this.share});

  final PoolShare share;

  @override
  State<_LimitsDialog> createState() => _LimitsDialogState();
}

class _LimitsDialogState extends State<_LimitsDialog> {
  late final TextEditingController _requests = TextEditingController(text: '${widget.share.requestsPerDay}');
  late final TextEditingController _tokens = TextEditingController(text: '${widget.share.tokensPerDay}');
  late final TextEditingController _concurrent = TextEditingController(text: '${widget.share.concurrent}');
  late final TextEditingController _reserve = TextEditingController(text: '${(widget.share.reserve * 100).round()}');

  @override
  void dispose() {
    for (final field in <TextEditingController>[_requests, _tokens, _concurrent, _reserve]) {
      field.dispose();
    }
    super.dispose();
  }

  int? _value(TextEditingController field, int min, int max) {
    final value = int.tryParse(field.text.trim());
    return value != null && value >= min && value <= max ? value : null;
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final requests = _value(_requests, 1, 100000);
    final tokens = _value(_tokens, 1000, 100000000);
    final concurrent = _value(_concurrent, 1, 20);
    final reserve = _value(_reserve, 0, 90);
    final valid = requests != null && tokens != null && concurrent != null && reserve != null;
    Widget field(TextEditingController controller, String label, {String? helper}) => Padding(
          padding: const EdgeInsets.only(bottom: AppSpacing.md),
          child: TextField(
            controller: controller,
            keyboardType: TextInputType.number,
            inputFormatters: <TextInputFormatter>[FilteringTextInputFormatter.digitsOnly],
            decoration: InputDecoration(labelText: label, helperText: helper, helperMaxLines: 2),
            onChanged: (_) => setState(() {}),
          ),
        );
    return AlertDialog(
      title: Text(t.poolLimitsTitle),
      content: SizedBox(
        width: 420,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            field(_requests, t.poolLimitRequests),
            field(_tokens, t.poolLimitTokens),
            field(_concurrent, t.poolLimitConcurrent),
            field(_reserve, t.poolLimitReserve, helper: t.poolLimitReserveHint),
            Text(t.poolLimitsBalanceHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.4)),
          ],
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(
          onPressed: valid
              ? () => Navigator.of(context).pop(<String, Object?>{'requestsPerDay': requests, 'tokensPerDay': tokens, 'concurrent': concurrent, 'reserve': reserve / 100})
              : null,
          child: Text(t.save),
        ),
      ],
    );
  }
}
