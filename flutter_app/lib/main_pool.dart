import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_pool_graph.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// The one explanation of what a pool means for privacy, shown on the pool
/// tab, when starting or joining, and behind the info button of a key's
/// pool option.
Future<void> showPoolTrustInfo(BuildContext context) {
  final t = AppL10n.of(context);
  final palette = neoRouterPaletteOf(context);
  return showDialog<void>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: Text(t.teamTabPool),
      content: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 440),
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

/// "3 partners · 2 online", or the pause, in a few words.
String poolSubtitle(AppL10n t, PoolInfo pool) {
  if (pool.paused) return t.homePoolPaused;
  if (pool.partners.isEmpty) return t.poolNoPartnersShort;
  return t.poolPartnersCount('${pool.partners.length}', '${pool.onlinePartners}');
}

/// The quick switch for one pool: the same control on the home page, in the
/// pool list and on a pool's page.
class PoolSwitch extends StatefulWidget {
  const PoolSwitch({super.key, required this.controller, required this.pool});

  final NeoRouterController controller;
  final PoolInfo pool;

  @override
  State<PoolSwitch> createState() => _PoolSwitchState();
}

class _PoolSwitchState extends State<PoolSwitch> {
  bool _busy = false;

  Future<void> _toggle(bool on) async {
    setState(() => _busy = true);
    try {
      await widget.controller.setPoolPaused(widget.pool.id, !on);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Switch(
        value: !widget.pool.paused,
        onChanged: _busy || !widget.controller.canManagePool ? null : _toggle,
      );
}

/// Picks connections as tiles: one tap selects, a tick shows it. With many
/// connections a filter narrows the list; "All" and "None" act on what is shown.
class ConnectionPicker extends StatefulWidget {
  const ConnectionPicker({super.key, required this.accounts, required this.selected, required this.onChanged});

  final List<ShareableAccount> accounts;
  final Set<String> selected;
  final ValueChanged<Set<String>> onChanged;

  @override
  State<ConnectionPicker> createState() => _ConnectionPickerState();
}

class _ConnectionPickerState extends State<ConnectionPicker> {
  static const int _filterFrom = 8;
  String _query = '';

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    if (widget.accounts.isEmpty) return InlineHint(message: t.poolShareNone);
    final shown = widget.accounts.where((account) => account.label.toLowerCase().contains(_query.toLowerCase())).toList();
    final ids = shown.map((account) => account.id).toSet();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Text(
                t.pickerSelected('${widget.selected.length}', '${widget.accounts.length}'),
                style: TextStyle(color: palette.textMuted, fontSize: 12.5, fontWeight: FontWeight.w600),
              ),
            ),
            TextButton(onPressed: () => widget.onChanged(<String>{...widget.selected, ...ids}), child: Text(t.pickerAll)),
            TextButton(onPressed: () => widget.onChanged(<String>{...widget.selected}..removeAll(ids)), child: Text(t.pickerNone)),
          ],
        ),
        if (widget.accounts.length >= _filterFrom) ...<Widget>[
          TextField(
            decoration: InputDecoration(isDense: true, prefixIcon: const Icon(Icons.search_rounded, size: 18), hintText: t.pickerSearch),
            onChanged: (value) => setState(() => _query = value.trim()),
          ),
          const SizedBox(height: AppSpacing.sm),
        ],
        LayoutBuilder(
          builder: (context, constraints) {
            final columns = constraints.maxWidth >= 560 ? 3 : constraints.maxWidth >= 360 ? 2 : 1;
            final width = (constraints.maxWidth - (columns - 1) * 8) / columns;
            return Wrap(
              spacing: 8,
              runSpacing: 8,
              children: <Widget>[
                for (final account in shown)
                  SizedBox(
                    width: width,
                    child: _ConnectionTile(
                      account: account,
                      selected: widget.selected.contains(account.id),
                      onTap: () => widget.onChanged(widget.selected.contains(account.id)
                          ? (<String>{...widget.selected}..remove(account.id))
                          : <String>{...widget.selected, account.id}),
                    ),
                  ),
              ],
            );
          },
        ),
      ],
    );
  }
}

class _ConnectionTile extends StatelessWidget {
  const _ConnectionTile({required this.account, required this.selected, required this.onTap});

  final ShareableAccount account;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Material(
      color: selected ? palette.accentSoft : palette.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.control),
        side: BorderSide(color: selected ? palette.accent.withValues(alpha: 0.55) : palette.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(10, 9, 10, 9),
          child: Row(
            children: <Widget>[
              ProviderAvatar(label: account.label, size: 28),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(account.label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: palette.text)),
                    Text(t.poolFreeModels('${account.freeModels}'), maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12, color: palette.textMuted)),
                  ],
                ),
              ),
              const SizedBox(width: 6),
              AnimatedSwitcher(
                duration: const Duration(milliseconds: 140),
                child: selected
                    ? Icon(Icons.check_circle_rounded, key: const ValueKey<bool>(true), size: 18, color: palette.accent)
                    : Icon(Icons.circle_outlined, key: const ValueKey<bool>(false), size: 18, color: palette.borderStrong),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// A dialog that fits any screen: title on top, a scrolling body, and the
/// actions always visible at the bottom.
class PoolDialogFrame extends StatelessWidget {
  const PoolDialogFrame({super.key, required this.title, required this.body, required this.actions, this.maxWidth = 620});

  final String title;
  final Widget body;
  final List<Widget> actions;
  final double maxWidth;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final size = MediaQuery.sizeOf(context);
    return Dialog(
      insetPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 24),
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: maxWidth, maxHeight: size.height * 0.88),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 22, 24, 12),
              child: Text(title, style: Theme.of(context).textTheme.titleLarge),
            ),
            Flexible(child: SingleChildScrollView(padding: const EdgeInsets.fromLTRB(24, 4, 24, 16), child: body)),
            Container(
              decoration: BoxDecoration(border: Border(top: BorderSide(color: palette.border))),
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 14),
              child: Wrap(alignment: WrapAlignment.end, spacing: 8, runSpacing: 8, children: actions),
            ),
          ],
        ),
      ),
    );
  }
}

/// The Pools tab of the Network page: pick a pool with its chip, then its
/// overview (the animated basin), what it shares, and its settings.
class PoolTab extends StatefulWidget {
  const PoolTab({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<PoolTab> createState() => _PoolTabState();
}

class _PoolTabState extends State<PoolTab> {
  bool _loading = true;
  String? _error;
  String? _poolId;

  NeoRouterController get controller => widget.controller;

  @override
  void initState() {
    super.initState();
    controller.addListener(_changed);
    _load().then((_) {
      final link = controller.pendingPoolInvite;
      if (link != null && mounted) {
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
    await controller.refreshPool();
    if (!mounted) return;
    setState(() {
      _loading = false;
      if (controller.pools == null) _error = AppL10n.of(context).poolLoadFailed;
    });
  }

  Future<void> _create() async {
    final created = await showDialog<PoolsSummary>(context: context, builder: (_) => _PoolFormDialog(controller: controller));
    if (created == null) return;
    controller.updatePools(created);
    setState(() => _poolId = created.createdId);
  }

  Future<void> _join([String? link]) async {
    final joined = await showDialog<PoolsSummary>(context: context, builder: (_) => _PoolFormDialog(controller: controller, joinLink: link ?? ''));
    if (joined == null) return;
    controller.updatePools(joined);
    setState(() => _poolId = joined.createdId);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final summary = controller.pools;
    final pools = summary?.pools ?? const <PoolInfo>[];
    final open = summary?.pool(_poolId) ?? (pools.isEmpty ? null : pools.first);
    final roomForMore = summary != null && pools.length < summary.maxPools;
    return AsyncBody(
      loading: _loading,
      error: _error,
      onRetry: _load,
      child: summary == null
          ? const SizedBox()
          : open == null
              ? EmptyState(
                  icon: Icons.hub_outlined,
                  title: t.poolEmptyTitle,
                  message: t.poolEmptyMessage,
                  action: controller.canManagePool ? _StartOrJoin(onCreate: _create, onJoin: _join, centered: true) : null,
                )
              : Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    Wrap(
                      spacing: 8,
                      runSpacing: 8,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: <Widget>[
                        for (final pool in pools) _PoolChip(pool: pool, selected: pool.id == open.id, onTap: () => setState(() => _poolId = pool.id)),
                        if (controller.canManagePool && roomForMore) ...<Widget>[
                          const SizedBox(width: 8),
                          _StartOrJoin(onCreate: _create, onJoin: _join),
                        ],
                      ],
                    ),
                    const SizedBox(height: AppSpacing.lg),
                    _PoolPage(key: ValueKey<String>(open.id), controller: controller, pool: open, onLeft: () => setState(() => _poolId = null)),
                  ],
                ),
    );
  }
}

class _PoolChip extends StatelessWidget {
  const _PoolChip({required this.pool, required this.selected, required this.onTap});

  final PoolInfo pool;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Semantics(
      button: true,
      selected: selected,
      child: Material(
        color: selected ? palette.accentSoft : Colors.transparent,
        shape: StadiumBorder(side: BorderSide(color: selected ? palette.accent : palette.borderStrong)),
        child: InkWell(
          customBorder: const StadiumBorder(),
          onTap: onTap,
          child: Container(
            height: 38,
            padding: const EdgeInsets.symmetric(horizontal: 14),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Container(width: 7, height: 7, decoration: BoxDecoration(shape: BoxShape.circle, color: pool.paused ? palette.textMuted : palette.success)),
                const SizedBox(width: 8),
                Text(pool.name, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: selected ? palette.text : palette.textSoft)),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

class _StartOrJoin extends StatelessWidget {
  const _StartOrJoin({required this.onCreate, required this.onJoin, this.centered = false});

  final VoidCallback onCreate;
  final VoidCallback onJoin;
  final bool centered;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      alignment: centered ? WrapAlignment.center : WrapAlignment.start,
      children: <Widget>[
        OutlinedButton.icon(onPressed: onJoin, icon: const Icon(Icons.link_rounded, size: 18), label: Text(t.poolJoin)),
        FilledButton.icon(onPressed: onCreate, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.poolCreate)),
      ],
    );
  }
}

enum _PoolSection { overview, sharing, settings }

/// One pool: a card with its name, switch and invite button, then its tabs.
class _PoolPage extends StatefulWidget {
  const _PoolPage({super.key, required this.controller, required this.pool, required this.onLeft});

  final NeoRouterController controller;
  final PoolInfo pool;
  final VoidCallback onLeft;

  @override
  State<_PoolPage> createState() => _PoolPageState();
}

class _PoolPageState extends State<_PoolPage> {
  _PoolSection _section = _PoolSection.overview;

  Future<void> _invite() async {
    final t = AppL10n.of(context);
    try {
      final link = await widget.controller.api.createPoolInvite(widget.pool.id);
      await widget.controller.refreshPool();
      if (!mounted) return;
      await showDialog<void>(
        context: context,
        builder: (dialogContext) => PoolDialogFrame(
          title: t.poolInviteTitle,
          maxWidth: 520,
          body: _CopyableLink(intro: t.poolInviteBody, link: link),
          actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.keySecretDone))],
        ),
      );
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final pool = widget.pool;
    final manage = widget.controller.canManagePool;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(pool.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: palette.text)),
                  const SizedBox(height: 2),
                  Text(<String>[if (pool.isHost) t.poolHostYou, poolSubtitle(t, pool)].join(' · '), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                ],
              ),
            ),
            if (manage && pool.isHost) ...<Widget>[
              OutlinedButton.icon(onPressed: _invite, icon: const Icon(Icons.person_add_alt_1_outlined, size: 18), label: Text(t.poolInvite)),
              const SizedBox(width: 12),
            ],
            PoolSwitch(controller: widget.controller, pool: pool),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        PageTabs<_PoolSection>(
          tabs: <({_PoolSection value, String label, int? count})>[
            (value: _PoolSection.overview, label: t.poolTabOverview, count: null),
            if (manage) (value: _PoolSection.sharing, label: t.poolTabSharing, count: pool.sharedAccountIds.length),
            if (manage) (value: _PoolSection.settings, label: t.poolTabSettings, count: null),
          ],
          selected: _section,
          onChanged: (section) => setState(() => _section = section),
        ),
        const SizedBox(height: AppSpacing.lg),
        switch (_section) {
          _PoolSection.overview => _PoolOverview(controller: widget.controller, pool: pool, onEditSharing: () => setState(() => _section = _PoolSection.sharing)),
          _PoolSection.sharing => _SharingSection(key: ValueKey<String>('share-${pool.id}'), controller: widget.controller, pool: pool),
          _PoolSection.settings => _PoolSettingsSection(controller: widget.controller, pool: pool, onLeft: widget.onLeft),
        },
      ],
    );
  }
}

Future<void> _apply(BuildContext context, NeoRouterController controller, Future<PoolsSummary> Function() action) async {
  try {
    controller.updatePools(await action());
  } on ApiException catch (error) {
    if (context.mounted) showSnack(context, error.message);
  }
}

/// A member of a pool as the overview shows it, from this instance's point
/// of view: what a partner "gave" is what it gave us, what it "took" is what
/// it took from us; our own numbers are the sums over all partners.
class _Member {
  _Member({required this.id, required this.name, required this.isYou, required this.state, required this.models, required this.connections, required this.gave, required this.took, required this.balance, this.partner});

  final String id;
  final String name;
  final bool isYou;
  final String state;
  final int models;
  final int? connections;
  final int gave;
  final int took;
  final String balance;
  final PoolPartner? partner;
}

String _ourBalance(int given, int taken) {
  if (given + taken < 10) return 'new';
  if (given > 2 * taken) return 'giving_more';
  if (taken > 2 * given) return 'taking_more';
  return 'balanced';
}

/// The animated basin with everyone around it, a panel about the member
/// picked, the pool's numbers and its open invites.
class _PoolOverview extends StatefulWidget {
  const _PoolOverview({required this.controller, required this.pool, required this.onEditSharing});

  final NeoRouterController controller;
  final PoolInfo pool;
  final VoidCallback onEditSharing;

  @override
  State<_PoolOverview> createState() => _PoolOverviewState();
}

class _PoolOverviewState extends State<_PoolOverview> {
  String _selected = 'you';

  List<_Member> _members(AppL10n t) {
    final pool = widget.pool;
    final shareable = widget.controller.pools?.shareable ?? const <ShareableAccount>[];
    final shared = shareable.where((account) => pool.sharedAccountIds.contains(account.id));
    final partners = pool.partners;
    final weekGiven = partners.fold<int>(0, (sum, partner) => sum + partner.week.givenRequests);
    final weekTaken = partners.fold<int>(0, (sum, partner) => sum + partner.week.takenRequests);
    return <_Member>[
      _Member(
        id: 'you',
        name: pool.selfName.isEmpty ? (widget.controller.user?.displayName ?? '') : pool.selfName,
        isYou: true,
        state: 'online',
        models: shared.fold<int>(0, (sum, account) => sum + account.freeModels),
        connections: pool.sharedAccountIds.length,
        gave: partners.fold<int>(0, (sum, partner) => sum + partner.today.givenRequests),
        took: partners.fold<int>(0, (sum, partner) => sum + partner.today.takenRequests),
        balance: _ourBalance(weekGiven, weekTaken),
      ),
      for (final partner in partners)
        _Member(
          id: partner.memberId,
          name: partner.name,
          isYou: false,
          state: partner.state,
          models: partner.models,
          connections: null,
          gave: partner.today.takenRequests,
          took: partner.today.givenRequests,
          // Their balance is ours turned round.
          balance: switch (partner.balance) {
            'giving_more' => 'taking_more',
            'taking_more' => 'giving_more',
            final other => other,
          },
          partner: partner,
        ),
    ];
  }

  String _balanceText(AppL10n t, _Member member) => switch (member.balance) {
        'balanced' => t.poolBalanceBalanced,
        'giving_more' => member.isYou ? t.poolBalanceGiving : t.balanceTheyGive,
        'taking_more' => member.isYou ? t.poolBalanceTaking : t.balanceTheyTake,
        _ => t.poolBalanceNew,
      };

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final pool = widget.pool;
    final members = _members(t);
    final totalModels = math.max(1, members.fold<int>(0, (sum, member) => sum + member.models));
    final you = members.first;
    // How much of our own daily share is still free (what partners may still take from us).
    final capacity = pool.share.requestsPerDay * math.max(1, pool.partners.length);
    final level = pool.partners.isEmpty ? 1.0 : (1 - you.took / capacity).clamp(0.0, 1.0);
    final selected = members.where((member) => member.id == _selected).firstOrNull ?? you;
    final graph = PoolGraph(
      members: <PoolGraphMember>[
        for (final member in members)
          PoolGraphMember(
            id: member.id,
            name: member.name,
            label: member.isYou ? t.poolMemberYou(member.name) : member.name,
            caption: member.connections != null ? t.memberCaptionYou('${member.connections}', '${member.models}') : t.memberCaptionPartner('${member.models}'),
            isYou: member.isYou,
            state: member.state,
            share: member.models / totalModels,
            gives: member.gave,
            takes: member.took,
          ),
      ],
      level: level,
      levelLabel: '${(level * 100).round()}%',
      levelCaption: t.graphFreeToday,
      footnote: t.graphRequestsToday(formatCount(you.gave + you.took)),
      active: !pool.paused,
      selectedId: selected.id,
      onSelect: (id) => setState(() => _selected = id),
      // Square-ish on a phone, roomy on a wide screen.
      height: MediaQuery.sizeOf(context).width < 700 ? 360 : 520,
    );
    final stats = <(String, String)>[
      (t.statOnline, t.statOnlineValue('${1 + pool.onlinePartners}', '${members.length}')),
      (t.statModels, formatCount(totalModels)),
      (t.statRequests, formatCount(you.gave + you.took)),
      (t.statBalance, _balanceText(t, you)),
    ];
    return LayoutBuilder(
      builder: (context, constraints) {
        final wide = constraints.maxWidth >= 980;
        final card = AppCard(
          padding: EdgeInsets.zero,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Padding(padding: EdgeInsets.fromLTRB(12, wide ? 20 : 14, 12, 4), child: graph),
              Divider(height: 1, color: palette.border),
              Padding(padding: const EdgeInsets.fromLTRB(20, 12, 20, 14), child: const _Legend()),
            ],
          ),
        );
        final panel = _MemberPanel(
          member: selected,
          totalModels: totalModels,
          maxVolume: math.max(1, members.fold<int>(1, (top, member) => math.max(top, math.max(member.gave, member.took)))),
          balance: _balanceText(t, selected),
          controller: widget.controller,
          pool: pool,
          onEditSharing: widget.onEditSharing,
        );
        final statGrid = GridView.count(
          crossAxisCount: wide ? 2 : 2,
          shrinkWrap: true,
          physics: const NeverScrollableScrollPhysics(),
          mainAxisSpacing: 12,
          crossAxisSpacing: 12,
          childAspectRatio: wide ? 2.1 : 2.4,
          children: <Widget>[
            for (final (label, value) in stats)
              AppCard(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisAlignment: MainAxisAlignment.center,
                  children: <Widget>[
                    Text(label, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 11.5, color: palette.textMuted)),
                    const SizedBox(height: 4),
                    Text(value, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 18, fontWeight: FontWeight.w700, color: palette.text)),
                  ],
                ),
              ),
          ],
        );
        final invites = _InviteList(controller: widget.controller, pool: pool);
        if (!wide) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[card, const SizedBox(height: AppSpacing.md), panel, const SizedBox(height: AppSpacing.md), statGrid, invites, const SizedBox(height: AppSpacing.md), InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined)],
          );
        }
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Expanded(child: card),
                const SizedBox(width: AppSpacing.lg),
                SizedBox(
                  width: 340,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[panel, const SizedBox(height: AppSpacing.md), statGrid, const SizedBox(height: AppSpacing.md), InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined)],
                  ),
                ),
              ],
            ),
            invites,
          ],
        );
      },
    );
  }
}

class _Legend extends StatelessWidget {
  const _Legend();

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final style = TextStyle(fontSize: 12, color: palette.textMuted);
    Widget item(Widget mark, String text) => Row(mainAxisSize: MainAxisSize.min, children: <Widget>[mark, const SizedBox(width: 6), Text(text, style: style)]);
    Widget dot(Color color) => Container(width: 8, height: 8, decoration: BoxDecoration(shape: BoxShape.circle, color: color));
    return Wrap(
      spacing: 18,
      runSpacing: 8,
      children: <Widget>[
        item(dot(PoolGraph.give), t.legendGives),
        item(dot(PoolGraph.take), t.legendTakes),
        item(SizedBox(width: 14, child: Text('- -', style: style.copyWith(height: 1))), t.legendOffline),
        item(Container(width: 12, height: 12, decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: PoolGraph.give, width: 2))), t.legendRing),
      ],
    );
  }
}

class _MemberPanel extends StatelessWidget {
  const _MemberPanel({required this.member, required this.totalModels, required this.maxVolume, required this.balance, required this.controller, required this.pool, required this.onEditSharing});

  final _Member member;
  final int totalModels;
  final int maxVolume;
  final String balance;
  final NeoRouterController controller;
  final PoolInfo pool;
  final VoidCallback onEditSharing;

  Future<void> _remove(BuildContext context, PoolPartner partner) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.poolRemoveMember, message: t.poolRemoveConfirm(partner.name), confirmLabel: t.poolRemoveMember, cancelLabel: t.cancel);
    if (confirmed && context.mounted) await _apply(context, controller, () => controller.api.removePoolMember(pool.id, partner.memberId));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final percent = (member.models * 100 / totalModels).round();
    final state = pool.paused && member.state == 'online' ? 'paused' : member.state;
    final partner = member.partner;
    Widget bar(String label, int value, Color color) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Row(children: <Widget>[
              Expanded(child: Text(label, style: TextStyle(fontSize: 12.5, color: palette.textSoft))),
              Text(t.panelRequests(formatCount(value)), style: TextStyle(fontSize: 12.5, color: palette.text, fontFeatures: const <FontFeature>[FontFeature.tabularFigures()])),
            ]),
            const SizedBox(height: 6),
            ClipRRect(
              borderRadius: BorderRadius.circular(4),
              child: LinearProgressIndicator(value: value / maxVolume, minHeight: 8, color: color, backgroundColor: palette.surfaceRaised),
            ),
          ],
        );
    return AppCard(
      padding: const EdgeInsets.all(20),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              ProviderAvatar(label: member.name, size: 44),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(member.isYou ? t.poolMemberYou(member.name) : member.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: palette.text)),
                    const SizedBox(height: 4),
                    StatusLabel(label: poolStateLabel(t, state), tone: poolStateTone(state)),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 14),
          Text(
            member.isYou ? t.panelSharesYou('${member.connections ?? 0}', '${member.models}', '$percent') : t.panelSharesPartner('${member.models}', '$percent'),
            style: TextStyle(fontSize: 13.5, color: palette.textSoft, height: 1.5),
          ),
          if (partner != null && partner.state == 'offline' && partner.problem != null) ...<Widget>[
            const SizedBox(height: 8),
            Text(partner.problem!, maxLines: 3, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: palette.warning, height: 1.4)),
          ],
          const SizedBox(height: 16),
          bar(t.panelGave, member.gave, PoolGraph.give),
          const SizedBox(height: 12),
          bar(t.panelTook, member.took, PoolGraph.take),
          const SizedBox(height: 16),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
            child: Row(children: <Widget>[
              Expanded(child: Text(t.panelBalance, style: TextStyle(fontSize: 12.5, color: palette.textSoft))),
              Text(balance, style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: palette.text)),
            ]),
          ),
          if (controller.canManagePool) ...<Widget>[
            const SizedBox(height: 14),
            if (member.isYou)
              Align(alignment: Alignment.centerLeft, child: TextButton(onPressed: onEditSharing, child: Text(t.panelEditSharing)))
            else if (partner != null && partner.linkId != null)
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: <Widget>[
                  OutlinedButton(
                    onPressed: () => _apply(context, controller, () => controller.api.setPartnerPaused(partner.linkId!, partner.state != 'paused')),
                    child: Text(partner.state == 'paused' ? t.poolResumePartner : t.poolPausePartner),
                  ),
                  if (pool.isHost)
                    TextButton(onPressed: () => _remove(context, partner), child: Text(t.poolRemoveMember, style: TextStyle(color: palette.error))),
                ],
              ),
          ],
        ],
      ),
    );
  }
}

class _InviteList extends StatelessWidget {
  const _InviteList({required this.controller, required this.pool});

  final NeoRouterController controller;
  final PoolInfo pool;

  @override
  Widget build(BuildContext context) {
    if (!controller.canManagePool || pool.invites.isEmpty) return const SizedBox.shrink();
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(top: 28),
      child: SettingsGroup(
        title: t.inviteLinksTitle,
        children: <Widget>[
          for (final invite in pool.invites)
            ListRow(
              leading: Icon(Icons.link_rounded, size: 18, color: palette.textSoft),
              title: t.poolInviteTitle,
              subtitle: t.inviteExpires(formatTime(DateTime.parse(invite['expiresAt'] as String).toLocal())),
              trailing: TextButton(
                onPressed: () async {
                  try {
                    await controller.api.revokePoolInvite(pool.id, invite['id'] as String);
                    await controller.refreshPool();
                  } on ApiException catch (error) {
                    if (context.mounted) showSnack(context, error.message);
                  }
                },
                child: Text(t.revoke, style: TextStyle(color: palette.error)),
              ),
            ),
        ],
      ),
    );
  }
}

/// What this pool gets: connections (saved together) and the per-partner share.
class _SharingSection extends StatefulWidget {
  const _SharingSection({super.key, required this.controller, required this.pool});

  final NeoRouterController controller;
  final PoolInfo pool;

  @override
  State<_SharingSection> createState() => _SharingSectionState();
}

class _SharingSectionState extends State<_SharingSection> {
  late Set<String> _selected = widget.pool.sharedAccountIds.toSet();
  bool _saving = false;

  bool get _changed {
    final saved = widget.pool.sharedAccountIds.toSet();
    return saved.length != _selected.length || !saved.containsAll(_selected);
  }

  Future<void> _save() async {
    setState(() => _saving = true);
    await _apply(context, widget.controller, () => widget.controller.api.updatePoolShare(widget.pool.id, <String, Object?>{'sharedAccountIds': _selected.toList()}));
    if (mounted) setState(() => _saving = false);
  }

  Future<void> _editLimits() async {
    final next = await showDialog<Map<String, Object?>>(context: context, builder: (_) => _LimitsDialog(share: widget.pool.share));
    if (next != null && mounted) await _apply(context, widget.controller, () => widget.controller.api.updatePoolShare(widget.pool.id, next));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final share = widget.pool.share;
    final accounts = widget.controller.pools?.shareable ?? const <ShareableAccount>[];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsGroup(
          title: t.poolShareTitle,
          description: t.poolShareHint,
          trailing: _changed
              ? Wrap(spacing: 8, children: <Widget>[
                  TextButton(onPressed: _saving ? null : () => setState(() => _selected = widget.pool.sharedAccountIds.toSet()), child: Text(t.poolDiscard)),
                  FilledButton(onPressed: _saving ? null : _save, child: Text(t.save)),
                ])
              : null,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
              child: ConnectionPicker(accounts: accounts, selected: _selected, onChanged: (next) => setState(() => _selected = next)),
            ),
          ],
        ),
        SettingsGroup(
          title: t.poolLimitsTitle,
          description: t.poolLimitsBalanceHint,
          children: <Widget>[
            SettingsRow(
              label: t.poolLimitsLabel,
              description: t.poolLimitsValue(formatCount(share.requestsPerDay), formatCount(share.tokensPerDay), '${share.concurrent}', '${(share.reserve * 100).round()}'),
              control: OutlinedButton(onPressed: _editLimits, child: Text(t.poolLimitsChange)),
            ),
          ],
        ),
        InlineHint(message: t.poolTosNotice),
      ],
    );
  }
}

class _PoolSettingsSection extends StatelessWidget {
  const _PoolSettingsSection({required this.controller, required this.pool, required this.onLeft});

  final NeoRouterController controller;
  final PoolInfo pool;
  final VoidCallback onLeft;

  Future<void> _leave(BuildContext context) async {
    final t = AppL10n.of(context);
    final closing = pool.isHost && pool.partners.isNotEmpty;
    final confirmed = await confirmAction(
      context,
      title: closing ? t.poolClose : t.poolLeave,
      message: closing ? t.poolCloseConfirm(pool.name) : t.poolLeaveConfirm(pool.name),
      confirmLabel: closing ? t.poolClose : t.poolLeave,
      cancelLabel: t.cancel,
    );
    if (!confirmed || !context.mounted) return;
    await _apply(context, controller, () => controller.api.leavePool(pool.id));
    onLeft();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final closing = pool.isHost && pool.partners.isNotEmpty;
    // One card: the tab already names it, and leaving is said once.
    return SettingsCardList(
      children: <Widget>[
        SettingsRow(label: t.poolSelfNameLabel, description: t.poolSelfNameHint, control: Text(pool.selfName, style: TextStyle(color: palette.text, fontWeight: FontWeight.w600))),
        SettingsRow(
          label: closing ? t.poolClose : t.poolLeave,
          description: closing ? t.poolCloseHint : t.poolLeaveHint,
          control: OutlinedButton(
            onPressed: () => _leave(context),
            style: OutlinedButton.styleFrom(foregroundColor: palette.error),
            child: Text(closing ? t.poolClose : t.poolLeave),
          ),
        ),
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

/// Starting a pool, or joining one with a link (joinLink set, maybe empty).
/// Agreeing to the privacy notice is the confirm button itself.
class _PoolFormDialog extends StatefulWidget {
  const _PoolFormDialog({required this.controller, this.joinLink});

  final NeoRouterController controller;
  final String? joinLink;

  @override
  State<_PoolFormDialog> createState() => _PoolFormDialogState();
}

class _PoolFormDialogState extends State<_PoolFormDialog> {
  late final TextEditingController _name = TextEditingController();
  late final TextEditingController _link = TextEditingController(text: widget.joinLink ?? '');
  late final TextEditingController _selfName = TextEditingController(text: widget.controller.user?.displayName ?? '');
  // Nothing is shared until chosen: some providers do not allow it.
  Set<String> _shared = <String>{};
  Map<String, dynamic>? _preview;
  bool _busy = false;
  String? _error;

  bool get _joining => widget.joinLink != null;
  List<ShareableAccount> get _accounts => widget.controller.pools?.shareable ?? const <ShareableAccount>[];

  @override
  void initState() {
    super.initState();
    if (_joining && _link.text.isNotEmpty) WidgetsBinding.instance.addPostFrameCallback((_) => _check());
  }

  @override
  void dispose() {
    _name.dispose();
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
        if (!mounted) return;
        setState(() {
          _preview = preview;
          if (preview['alreadyMember'] == true) _error = AppL10n.of(context).poolAlreadyMember;
        });
      });

  Future<void> _submit() => _run(() async {
        final result = _joining
            ? await widget.controller.api.joinPool(_link.text.trim(), _selfName.text.trim(), _shared.toList())
            : await widget.controller.api.createPool(_name.text.trim(), _selfName.text.trim(), _shared.toList());
        if (mounted) Navigator.of(context).pop(result);
      });

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final preview = _preview;
    final needsLink = _joining && preview == null;
    final previewName = (preview?['pool'] as Map?)?['name'] as String? ?? '';
    final ready = !_busy && _selfName.text.trim().isNotEmpty && (_joining ? preview != null && preview['alreadyMember'] != true : _name.text.trim().isNotEmpty);
    return PoolDialogFrame(
      title: _joining ? (preview == null ? t.poolJoinTitle : t.poolJoinPreview(previewName)) : t.poolCreateTitle,
      body: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (needsLink)
            TextField(
              controller: _link,
              autofocus: true,
              decoration: InputDecoration(labelText: t.poolLinkLabel),
              onChanged: (_) => setState(() {}),
              onSubmitted: (_) => _check(),
            )
          else ...<Widget>[
            if (preview != null) ...<Widget>[
              _PreviewCard(preview: preview),
              const SizedBox(height: AppSpacing.lg),
            ],
            LayoutBuilder(
              builder: (context, constraints) {
                final fields = <Widget>[
                  if (!_joining) TextField(controller: _name, autofocus: true, decoration: InputDecoration(labelText: t.poolNameLabel), onChanged: (_) => setState(() {})),
                  TextField(controller: _selfName, decoration: InputDecoration(labelText: t.poolSelfNameLabel, helperText: t.poolSelfNameHint), onChanged: (_) => setState(() {})),
                ];
                if (constraints.maxWidth < 480 || fields.length == 1) {
                  return Column(children: <Widget>[for (final field in fields) Padding(padding: const EdgeInsets.only(bottom: AppSpacing.sm), child: field)]);
                }
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[Expanded(child: fields[0]), const SizedBox(width: AppSpacing.sm), Expanded(child: fields[1])],
                );
              },
            ),
            const SizedBox(height: AppSpacing.lg),
            Text(t.poolShareTitle, style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
            const SizedBox(height: 2),
            Text(t.poolShareHint, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.4)),
            const SizedBox(height: AppSpacing.sm),
            ConnectionPicker(accounts: _accounts, selected: _shared, onChanged: (next) => setState(() => _shared = next)),
            const SizedBox(height: AppSpacing.md),
            InlineHint(message: t.poolTosNotice),
            const SizedBox(height: AppSpacing.sm),
            InlineHint(message: t.poolTrustNotice, icon: Icons.shield_outlined),
          ],
          if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
        ],
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        if (needsLink)
          FilledButton(onPressed: _busy || _link.text.trim().isEmpty ? null : _check, child: Text(t.poolCheckLink))
        else
          FilledButton(onPressed: ready ? _submit : null, child: Text(_joining ? t.poolAgreeJoin : t.poolAgreeCreate)),
      ],
    );
  }
}

class _PreviewCard extends StatelessWidget {
  const _PreviewCard({required this.preview});

  final Map<String, dynamic> preview;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final host = (preview['host'] as Map?)?['name'] as String? ?? '';
    final members = ((preview['members'] as List?) ?? const <Object>[]).map((item) => '${(item as Map)['name']}').toList();
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.card), border: Border.all(color: palette.border)),
      child: Row(
        children: <Widget>[
          Icon(Icons.hub_outlined, color: palette.accent),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(t.poolJoinMembers(host, '${members.length}'), style: TextStyle(fontWeight: FontWeight.w600, color: palette.text)),
                if (members.isNotEmpty) ...<Widget>[
                  const SizedBox(height: 2),
                  Text(members.join(', '), maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Which pools a connection is shared in (from its menu on the connections page).
Future<void> showSharePoolsDialog(BuildContext context, NeoRouterController controller, String accountId, String accountLabel) {
  return showDialog<void>(context: context, builder: (_) => _SharePoolsDialog(controller: controller, accountId: accountId, accountLabel: accountLabel));
}

class _SharePoolsDialog extends StatefulWidget {
  const _SharePoolsDialog({required this.controller, required this.accountId, required this.accountLabel});

  final NeoRouterController controller;
  final String accountId;
  final String accountLabel;

  @override
  State<_SharePoolsDialog> createState() => _SharePoolsDialogState();
}

class _SharePoolsDialogState extends State<_SharePoolsDialog> {
  final Set<String> _busy = <String>{};

  Future<void> _set(PoolInfo pool, bool shared) async {
    setState(() => _busy.add(pool.id));
    await _apply(context, widget.controller, () => widget.controller.api.setAccountPoolShare(pool.id, widget.accountId, shared));
    if (mounted) setState(() => _busy.remove(pool.id));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final pools = widget.controller.pools?.pools ?? const <PoolInfo>[];
    return PoolDialogFrame(
      title: t.poolShareDialogTitle(widget.accountLabel),
      maxWidth: 480,
      body: SettingsCardList(
        children: <Widget>[
          for (final pool in pools)
            SettingsRow(
              label: pool.name,
              description: poolSubtitle(t, pool),
              control: Switch(value: pool.sharedAccountIds.contains(widget.accountId), onChanged: _busy.contains(pool.id) ? null : (on) => _set(pool, on)),
            ),
        ],
      ),
      actions: <Widget>[FilledButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.close))],
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
    return PoolDialogFrame(
      title: t.poolLimitsTitle,
      maxWidth: 440,
      body: Column(
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
