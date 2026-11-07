import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';
import 'package:url_launcher/url_launcher.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_dashboard.dart';
import 'main_pool.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> _categoryOrder = <String>['free', 'plan', 'aggregator', 'paid', 'search', 'weather', 'local', 'custom'];

String categoryLabel(AppL10n t, String category) => switch (category) {
  'free' => t.categoryFree,
  'aggregator' => t.categoryAggregator,
  'paid' => t.categoryPaid,
  'local' => t.categoryLocal,
  'custom' => t.categoryCustom,
  'search' => t.categorySearch,
  'weather' => t.categoryWeather,
  'plan' => t.categoryPlan,
  _ => t.categoryCustom,
};

enum _ProvidersTab { connected, limits }

/// Connections: the provider accounts NeoRouter may use, and their limits.
class ProvidersScreen extends StatefulWidget {
  const ProvidersScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ProvidersScreen> createState() => _ProvidersScreenState();
}

class _ProvidersScreenState extends State<ProvidersScreen> {
  List<ProviderAccount> _accounts = <ProviderAccount>[];
  List<ProviderDefinition> _definitions = <ProviderDefinition>[];
  final Set<String> _busy = <String>{};
  bool _loading = true;
  String? _error;
  late _ProvidersTab _tab = widget.controller.savedTab('providers', _ProvidersTab.values, _ProvidersTab.connected);

  @override
  void initState() {
    super.initState();
    _load();
    WidgetsBinding.instance.addPostFrameCallback((_) => _showOAuthResult());
  }

  void _showOAuthResult() {
    final controller = widget.controller;
    if (!mounted || (controller.oauthStatus == null && controller.oauthError == null)) return;
    final t = AppL10n.of(context);
    showSnack(context, controller.oauthError != null ? t.openRouterFailed(controller.oauthError!) : t.openRouterConnected);
    controller.consumeOAuthResult();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.accounts(), if (_definitions.isEmpty) api.definitions()]);
      setState(() {
        _accounts = results[0] as List<ProviderAccount>;
        if (results.length > 1) _definitions = results[1] as List<ProviderDefinition>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _run(String accountId, Future<String?> Function() action) async {
    setState(() => _busy.add(accountId));
    try {
      final message = await action();
      if (mounted && message != null) showSnack(context, message);
      await _load();
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    } finally {
      if (mounted) setState(() => _busy.remove(accountId));
    }
  }

  // Accounts grouped by provider, in the order the first of each appears.
  List<List<ProviderAccount>> _groups() {
    final groups = <String, List<ProviderAccount>>{};
    for (final account in _accounts) {
      groups.putIfAbsent(account.providerKey, () => <ProviderAccount>[]).add(account);
    }
    return groups.values.toList();
  }

  Widget _row(ProviderAccount account, bool canManage) => _AccountRow(
    account: account,
    busy: _busy.contains(account.id),
    showCreator: widget.controller.showCreators,
    canManage: canManage,
    canDelete: widget.controller.can('providers.delete'),
    // "Share in pool" only while in a pool, for the owner, on connections with free models.
    canPoolShare: widget.controller.inPool && widget.controller.canManagePool && account.freeModelCount > 0,
    onToggle: (enabled) => _run(account.id, () async {
      await widget.controller.api.updateAccount(account.id, <String, Object?>{'enabled': enabled});
      return null;
    }),
    onAction: (action) => _action(account, action),
  );

  Future<void> _add() async {
    final definition = await showDialog<ProviderDefinition>(
      context: context,
      builder: (_) => _ProviderGallery(definitions: _definitions, connected: <String>{for (final account in _accounts) account.definitionId}),
    );
    if (definition == null || !mounted) return;
    final added = await showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition),
    );
    if (added != null && mounted) {
      showSnack(context, added);
      await _load();
    }
  }

  // A second key for the same provider: NeoRouter spreads requests over both.
  Future<void> _addAnother(ProviderAccount account) async {
    final definition = _definitions.where((item) => item.id == account.definitionId).firstOrNull;
    if (definition == null) return;
    final added = await showDialog<String>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition),
    );
    if (added != null && mounted) {
      showSnack(context, added);
      await _load();
    }
  }

  Future<void> _edit(ProviderAccount account) async {
    final definition = _definitions.where((item) => item.id == account.definitionId).firstOrNull;
    if (definition == null) return;
    final saved = await showDialog<String>(
      context: context,
      builder: (_) => _ProviderForm(controller: widget.controller, definition: definition, account: account),
    );
    if (saved != null && mounted) {
      showSnack(context, saved);
      await _load();
    }
  }

  Future<void> _action(ProviderAccount account, _AccountAction action) async {
    final t = AppL10n.of(context);
    final api = widget.controller.api;
    switch (action) {
      case _AccountAction.check:
        await _run(account.id, () async {
          final result = await api.validateAccount(account.id);
          return (result['validation'] as Map?)?['message'] as String? ?? t.statusHealthy;
        });
      case _AccountAction.sync:
        await _run(account.id, () async {
          final result = await api.syncAccount(account.id);
          return t.syncedModels('${(result['sync'] as Map?)?['models'] ?? 0}');
        });
      case _AccountAction.balance:
        await _run(account.id, () async {
          final items = ((await api.probeQuota(account.id))['quota'] as List).cast<Map>();
          if (items.isEmpty) return t.quotaProbeEmpty;
          return items.map((item) => t.quotaProbeResult(metricLabel(t, '${item['metric']}'), '${item['remaining']}')).join(' · ');
        });
      case _AccountAction.fixed:
        await _run(account.id, () async {
          final result = await api.markAccountFixed(account.id);
          final validation = result['validation'] as Map?;
          final failing = validation != null && validation['status'] == 'invalid';
          return failing ? t.markFixedStillFailing('${validation['message'] ?? ''}') : t.markFixedDone;
        });
      case _AccountAction.poolShare:
        await showSharePoolsDialog(context, widget.controller, account.id, account.label);
        await _load();
      case _AccountAction.edit:
        await _edit(account);
      case _AccountAction.another:
        await _addAnother(account);
      case _AccountAction.remove:
        final confirmed = await confirmAction(context, title: t.removeProvider, message: t.removeProviderConfirm(account.label), confirmLabel: t.removeProvider, cancelLabel: t.cancel);
        if (confirmed) {
          await _run(account.id, () async {
            await api.deleteAccount(account.id);
            return null;
          });
        }
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final controller = widget.controller;
    final canManage = controller.can('providers.manage');
    // The limits tab reads usage, which a team member may not be allowed to see.
    final showLimits = controller.can('activity.view');
    return PageScaffold(
      header: ScreenHeader(
        title: t.providersTitle,
        description: t.providersDescription,
        trailing: canManage ? FilledButton.icon(onPressed: _definitions.isEmpty ? null : _add, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.addProvider)) : null,
      ),
      children: <Widget>[
        if (_accounts.isNotEmpty && showLimits)
          PageTabs<_ProvidersTab>(
            tabs: <({_ProvidersTab value, String label, int? count})>[
              (value: _ProvidersTab.connected, label: t.tabConnected, count: _accounts.length),
              (value: _ProvidersTab.limits, label: t.tabLimits, count: null),
            ],
            selected: _tab,
            onChanged: (value) {
              widget.controller.rememberChoice('providers', value);
              setState(() => _tab = value);
            },
          ),
        if (_tab == _ProvidersTab.limits && _accounts.isNotEmpty && showLimits)
          _LimitsView(controller: widget.controller)
        else
          AsyncBody(
            loading: _loading,
            error: _error,
            onRetry: _load,
            child: _accounts.isEmpty
                ? AppCard(
                    child: EmptyState(
                      icon: Icons.link_rounded,
                      title: t.noProvidersTitle,
                      message: t.noProvidersMessage,
                      action: canManage ? FilledButton.icon(onPressed: _definitions.isEmpty ? null : _add, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.addProvider)) : null,
                    ),
                  )
                : SettingsCardList(
                    children: <Widget>[
                      // Several accounts with one provider show as that one provider.
                      for (final group in _groups())
                        if (group.length == 1)
                          _row(group.first, canManage)
                        else
                          _ProviderGroup(
                            accounts: group,
                            canManage: canManage,
                            busy: group.any((account) => _busy.contains(account.id)),
                            onToggleAll: (enabled) async {
                              for (final account in group) {
                                if (account.enabled != enabled) {
                                  await _run(account.id, () async {
                                    await widget.controller.api.updateAccount(account.id, <String, Object?>{'enabled': enabled});
                                    return null;
                                  });
                                }
                              }
                            },
                            onAddAnother: () => _action(group.first, _AccountAction.another),
                            rows: <Widget>[for (final account in group) _row(account, canManage)],
                          ),
                    ],
                  ),
          ),
      ],
    );
  }
}

enum _AccountAction { check, sync, balance, fixed, edit, another, poolShare, remove }

/// A provider connected with several accounts: one row for the provider, its
/// accounts in a dropdown under it.
class _ProviderGroup extends StatefulWidget {
  const _ProviderGroup({required this.accounts, required this.rows, required this.canManage, required this.busy, required this.onToggleAll, required this.onAddAnother});

  final List<ProviderAccount> accounts;
  final List<Widget> rows;
  final bool canManage;
  final bool busy;
  final Future<void> Function(bool enabled) onToggleAll;
  final VoidCallback onAddAnother;

  @override
  State<_ProviderGroup> createState() => _ProviderGroupState();
}

class _ProviderGroupState extends State<_ProviderGroup> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final accounts = widget.accounts;
    final first = accounts.first;
    final enabled = accounts.where((account) => account.enabled).toList();
    final working = enabled.where((account) => account.status == 'healthy').length;
    final problems = enabled.length - working;
    final models = accounts.map((account) => account.modelCount).fold<int>(0, math.max);
    final free = accounts.map((account) => account.freeModelCount).fold<int>(0, math.max);
    final tone = enabled.isEmpty ? PillTone.neutral : problems == 0 ? PillTone.success : working == 0 ? PillTone.error : PillTone.warning;
    final status = enabled.isEmpty ? t.modeOff : problems == 0 ? t.providerAccountsWorking('$working') : t.providerAccountsProblems('$problems');
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        InkWell(
          onTap: () => setState(() => _open = !_open),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(20, 16, 8, 16),
            child: Row(
              children: <Widget>[
                ProviderAvatar(label: first.providerName, size: 38),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(first.providerName, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text)),
                      const SizedBox(height: 3),
                      Text(
                        <String>[t.providerAccountsCount('${accounts.length}'), t.homeModelsLine('$models', '$free')].join(' · '),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 13, color: palette.textMuted),
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 12),
                if (MediaQuery.sizeOf(context).width >= 700) ...<Widget>[StatusLabel(label: status, tone: tone), const SizedBox(width: 16)] else ...<Widget>[
                  ToneDot(tone: tone),
                  const SizedBox(width: 10),
                ],
                if (widget.busy)
                  const Padding(padding: EdgeInsets.all(14), child: SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)))
                else
                  Switch(value: enabled.isNotEmpty, onChanged: widget.canManage ? widget.onToggleAll : null),
                IconButton(
                  tooltip: _open ? t.providerHideAccounts : t.providerShowAccounts,
                  onPressed: () => setState(() => _open = !_open),
                  icon: Icon(_open ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 22),
                ),
              ],
            ),
          ),
        ),
        if (_open)
          Container(
            margin: const EdgeInsets.fromLTRB(32, 0, 12, 12),
            decoration: BoxDecoration(
              color: palette.surfaceRaised,
              borderRadius: BorderRadius.circular(AppRadius.control),
              border: Border.all(color: palette.border),
            ),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: <Widget>[
                for (var i = 0; i < widget.rows.length; i++) ...<Widget>[if (i > 0) Divider(height: 1, color: palette.border), widget.rows[i]],
                if (widget.canManage) ...<Widget>[
                  Divider(height: 1, color: palette.border),
                  Align(
                    alignment: Alignment.centerLeft,
                    child: Padding(
                      padding: const EdgeInsets.all(8),
                      child: TextButton.icon(onPressed: widget.onAddAnother, icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.addAnotherAccount)),
                    ),
                  ),
                ],
              ],
            ),
          ),
      ],
    );
  }
}

/// One connection: who, how many models, whether it is free and working.
class _AccountRow extends StatelessWidget {
  const _AccountRow({
    required this.account,
    required this.busy,
    required this.onToggle,
    required this.onAction,
    this.showCreator = false,
    this.canManage = true,
    this.canDelete = true,
    this.canPoolShare = false,
  });

  final ProviderAccount account;
  final bool busy;
  final bool showCreator;
  final bool canManage;
  final bool canDelete;
  final bool canPoolShare;
  final ValueChanged<bool> onToggle;
  final ValueChanged<_AccountAction> onAction;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (statusText, tone) = accountStatus(t, account);
    final billing = account.billingMode == 'plan'
        ? t.accountPrepaidPlan
        : account.billingMode == 'plan_paid'
        ? t.accountPlanThenPaid
        : account.billingMode != 'free_tier'
        ? (account.freeModelCount > 0 ? t.accountPaidWithFree('${account.freeModelCount}') : t.accountPaidPlan)
        : !account.billingVerifiable
        ? (account.freeTierAll ? t.accountFreePlan : t.accountFreeModelsOnly)
        : switch (account.billingState) {
            'free' => t.accountFreeVerified,
            'paid' => t.accountBillingActive,
            _ => t.accountNotVerified,
          };
    final problem = account.enabled && account.status != 'healthy' && account.statusMessage != null ? account.statusMessage : null;
    return Opacity(
      opacity: account.enabled ? 1 : 0.6,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 16, 8, 16),
        child: Row(
          children: <Widget>[
            ProviderAvatar(label: account.label, size: 38),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    account.label,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text),
                  ),
                  const SizedBox(height: 3),
                  Text(
                    <String>[
                      t.homeModelsLine('${account.modelCount}', '${account.freeModelCount}'),
                      billing,
                      if (showCreator) createdByLabel(t, account.createdBy),
                    ].join(' · '),
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(fontSize: 13, color: palette.textMuted),
                  ),
                  if (problem != null) ...<Widget>[
                    const SizedBox(height: 4),
                    Text(
                      problem,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 12.5, color: palette.error),
                    ),
                  ],
                ],
              ),
            ),
            const SizedBox(width: 12),
            if (MediaQuery.sizeOf(context).width >= 700) ...<Widget>[StatusLabel(label: statusText, tone: tone), const SizedBox(width: 16)] else ...<Widget>[
              ToneDot(tone: tone),
              const SizedBox(width: 10),
            ],
            if (busy)
              const Padding(
                padding: EdgeInsets.all(14),
                child: SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2)),
              )
            else
              Switch(value: account.enabled, onChanged: canManage ? onToggle : null),
            if (canManage || canDelete)
              PopupMenuButton<_AccountAction>(
                tooltip: t.moreActions,
                enabled: !busy,
                icon: const Icon(Icons.more_horiz_rounded, size: 20),
                onSelected: onAction,
                itemBuilder: (context) => <PopupMenuEntry<_AccountAction>>[
                  if (canManage) ...<PopupMenuEntry<_AccountAction>>[
                    _menuItem(_AccountAction.check, Icons.verified_user_outlined, t.revalidate),
                    _menuItem(_AccountAction.sync, Icons.sync_rounded, t.syncModels),
                    _menuItem(_AccountAction.balance, Icons.account_balance_wallet_outlined, t.probeQuota),
                    _menuItem(_AccountAction.fixed, Icons.build_circle_outlined, t.markFixed),
                    _menuItem(_AccountAction.edit, Icons.edit_outlined, t.editProvider),
                    _menuItem(_AccountAction.another, Icons.add_rounded, t.addAnotherAccount),
                  ],
                  if (canPoolShare)
                    _menuItem(_AccountAction.poolShare, Icons.hub_outlined, account.sharedInPools.isEmpty ? t.poolShareInPools : '${t.poolShareInPools} (${account.sharedInPools.length})'),
                  if (canManage && canDelete) const PopupMenuDivider(),
                  if (canDelete) _menuItem(_AccountAction.remove, Icons.delete_outline_rounded, t.removeProvider, destructive: palette.error),
                ],
              ),
          ],
        ),
      ),
    );
  }

  static PopupMenuItem<_AccountAction> _menuItem(_AccountAction value, IconData icon, String label, {Color? destructive}) => PopupMenuItem<_AccountAction>(
    value: value,
    height: 40,
    child: Row(
      children: <Widget>[
        Icon(icon, size: 17, color: destructive),
        const SizedBox(width: 12),
        Text(label, style: TextStyle(fontSize: 13.5, color: destructive)),
      ],
    ),
  );
}

/// The cost tag of a provider, the same in the gallery and the form.
(String, PillTone, IconData) providerCostTag(AppL10n t, ProviderDefinition definition) => switch (definition.costKind) {
  'free' => (t.costTagFree, PillTone.success, Icons.savings_outlined),
  'free_models' => (t.costTagFreeModels, PillTone.accent, Icons.savings_outlined),
  'plan' => (t.costTagPlan, PillTone.info, Icons.workspace_premium_outlined),
  _ => (t.costTagPayg, PillTone.neutral, Icons.payments_outlined),
};

enum _GalleryFilter { all, free, plans, payg, tools, own }

/// Picking a provider: a search, a few filters and tiles that say at a glance
/// what each one costs and whether it is connected already.
class _ProviderGallery extends StatefulWidget {
  const _ProviderGallery({required this.definitions, required this.connected});

  final List<ProviderDefinition> definitions;
  final Set<String> connected;

  @override
  State<_ProviderGallery> createState() => _ProviderGalleryState();
}

class _ProviderGalleryState extends State<_ProviderGallery> {
  String _query = '';
  _GalleryFilter _filter = _GalleryFilter.all;

  bool _passes(ProviderDefinition item) => switch (_filter) {
    _GalleryFilter.all => true,
    _GalleryFilter.free => item.costKind == 'free' || item.costKind == 'free_models',
    _GalleryFilter.plans => item.category == 'plan',
    _GalleryFilter.payg => item.costKind == 'payg' && const <String>['paid', 'aggregator', 'free'].contains(item.category),
    _GalleryFilter.tools => item.category == 'search' || item.category == 'weather',
    _GalleryFilter.own => item.category == 'local' || item.category == 'custom',
  };

  String _label(AppL10n t, _GalleryFilter filter) => switch (filter) {
    _GalleryFilter.all => t.galleryFilterAll,
    _GalleryFilter.free => t.galleryFilterFree,
    _GalleryFilter.plans => t.galleryFilterPlans,
    _GalleryFilter.payg => t.galleryFilterPayg,
    _GalleryFilter.tools => t.galleryFilterTools,
    _GalleryFilter.own => t.galleryFilterOwn,
  };

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final query = _query.toLowerCase();
    final matches = widget.definitions
        .where((item) => query.isEmpty || item.label.toLowerCase().contains(query) || item.id.contains(query) || item.description.toLowerCase().contains(query))
        .where(_passes)
        .toList();
    final order = <String, int>{for (var i = 0; i < _categoryOrder.length; i++) _categoryOrder[i]: i};
    matches.sort(
      (a, b) =>
          (order[a.category] ?? 99).compareTo(order[b.category] ?? 99) != 0 ? (order[a.category] ?? 99).compareTo(order[b.category] ?? 99) : a.label.toLowerCase().compareTo(b.label.toLowerCase()),
    );
    return DialogFrame(
      title: t.chooseProvider,
      maxWidth: 820,
      subtitle: Text(t.chooseProviderHint, style: TextStyle(color: palette.textSoft, fontSize: 13.5)),
      headerTrailing: IconButton(tooltip: t.close, onPressed: () => Navigator.of(context).pop(), icon: const Icon(Icons.close_rounded, size: 20)),
      headerBottom: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          TextField(
            autofocus: true,
            decoration: InputDecoration(prefixIcon: const Icon(Icons.search_rounded, size: 20), hintText: t.searchProviders, isDense: true),
            onChanged: (value) => setState(() => _query = value),
          ),
          const SizedBox(height: AppSpacing.sm),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: <Widget>[
                for (final filter in _GalleryFilter.values) ...<Widget>[
                  ChoiceChip(label: Text(_label(t, filter)), selected: _filter == filter, onSelected: (_) => setState(() => _filter = filter)),
                  const SizedBox(width: 6),
                ],
              ],
            ),
          ),
        ],
      ),
      bodyPadding: const EdgeInsets.fromLTRB(24, 0, 24, 20),
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(t.galleryCount('${matches.length}'), style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
          const SizedBox(height: AppSpacing.sm),
          if (matches.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 24),
              child: InlineHint(message: t.galleryNoMatch),
            )
          else
            LayoutBuilder(
              builder: (context, constraints) {
                final columns = constraints.maxWidth >= 560 ? 2 : 1;
                final width = (constraints.maxWidth - (columns - 1) * 10) / columns;
                return Wrap(
                  spacing: 10,
                  runSpacing: 10,
                  children: <Widget>[
                    for (final definition in matches)
                      SizedBox(
                        width: width,
                        child: _ProviderTile(definition: definition, connected: widget.connected.contains(definition.id), onTap: () => Navigator.of(context).pop(definition)),
                      ),
                  ],
                );
              },
            ),
        ],
      ),
    );
  }
}

class _ProviderTile extends StatelessWidget {
  const _ProviderTile({required this.definition, required this.connected, required this.onTap});

  final ProviderDefinition definition;
  final bool connected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (costLabel, costTone, costIcon) = providerCostTag(t, definition);
    return Material(
      color: palette.surface,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(AppRadius.card),
        side: BorderSide(color: palette.border),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(14),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              ProviderAvatar(label: definition.label, size: 36),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(
                      definition.label,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text),
                    ),
                    const SizedBox(height: 3),
                    SizedBox(
                      height: 34,
                      child: Text(
                        definition.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.35),
                      ),
                    ),
                    const SizedBox(height: 8),
                    Wrap(
                      spacing: 6,
                      runSpacing: 6,
                      children: <Widget>[
                        MetaPill(label: costLabel, tone: costTone, icon: costIcon),
                        MetaPill(label: categoryLabel(t, definition.category)),
                        if (connected) MetaPill(label: t.costTagConnected, icon: Icons.check_rounded),
                      ],
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// What a provider costs, in a sentence, with where and when that was checked.
class _CostSummary extends StatelessWidget {
  const _CostSummary({required this.definition});

  final ProviderDefinition definition;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (_, _, icon) = providerCostTag(t, definition);
    final summary = switch (definition.costKind) {
      'free' => t.costSummaryFree,
      'free_models' => t.costSummaryFreeModels,
      'plan' => t.costSummaryPlan,
      _ => t.costSummaryPayg,
    };
    final checked = definition.billingCheckedAt;
    final source = definition.billingSource;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.card)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(icon, size: 20, color: definition.costKind == 'payg' ? palette.textSoft : palette.accent),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text(
                  summary,
                  style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
                ),
                if (definition.billingNote.isNotEmpty) ...<Widget>[const SizedBox(height: 4), Text(definition.billingNote, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.45))],
                if (checked != null || source != null) ...<Widget>[
                  const SizedBox(height: 6),
                  Wrap(
                    spacing: 10,
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: <Widget>[
                      if (checked != null) Text(t.costChecked(checked), style: TextStyle(fontSize: 12, color: palette.textMuted)),
                      if (source != null)
                        InkWell(
                          onTap: () => launchUrl(Uri.parse(source)),
                          child: Text(
                            t.costSource,
                            style: TextStyle(fontSize: 12, color: palette.accent, decoration: TextDecoration.underline, decorationColor: palette.accent),
                          ),
                        ),
                    ],
                  ),
                ],
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Create (account == null) or edit a provider account.
class _ProviderForm extends StatefulWidget {
  const _ProviderForm({required this.controller, required this.definition, this.account});

  final NeoRouterController controller;
  final ProviderDefinition definition;
  final ProviderAccount? account;

  @override
  State<_ProviderForm> createState() => _ProviderFormState();
}

class _ProviderFormState extends State<_ProviderForm> {
  late final TextEditingController _label = TextEditingController(text: widget.account?.label ?? widget.definition.label);
  late final TextEditingController _slug = TextEditingController(text: widget.account?.slug ?? widget.definition.id.replaceAll('_', '-'));
  late final TextEditingController _secret = TextEditingController();
  late final TextEditingController _baseUrl = TextEditingController(text: widget.account?.baseUrl ?? widget.definition.baseUrl);
  late final Map<String, TextEditingController> _fields = <String, TextEditingController>{
    for (final field in widget.definition.fields) field.key: TextEditingController(text: '${(widget.account?.raw['options'] as Map?)?[field.key] ?? ''}'),
  };
  late final Map<String, dynamic> _savedOptions = Map<String, dynamic>.from((widget.account?.raw['options'] as Map?) ?? <String, Object>{});
  late final TextEditingController _authHeader = TextEditingController(text: '${_savedOptions['authHeader'] ?? ''}');
  late final TextEditingController _modelsPath = TextEditingController(text: '${_savedOptions['modelsPath'] ?? ''}');
  late final TextEditingController _manualModels = TextEditingController(text: ((_savedOptions['manualModels'] as List?) ?? const <Object>[]).join('\n'));
  late final TextEditingController _headers = TextEditingController(
    text: ((_savedOptions['headers'] as Map?) ?? const <String, Object>{}).entries.map((entry) => '${entry.key}: ${entry.value}').join('\n'),
  );
  late String _billing = widget.account?.billingMode ?? widget.definition.defaultBillingMode;
  bool _saving = false;
  bool _probing = false;
  bool _connecting = false;
  // ChatGPT device sign-in in progress: the code to show and the poll timer.
  Map<String, dynamic>? _deviceLogin;
  Timer? _devicePoll;
  bool _advanced = false;
  Map<String, dynamic>? _probe;
  String? _error;

  bool get _editing => widget.account != null;
  bool get _custom => widget.definition.editableBaseUrl;

  @override
  void dispose() {
    for (final controller in <TextEditingController>[_label, _slug, _secret, _baseUrl, _authHeader, _modelsPath, _manualModels, _headers, ..._fields.values]) {
      controller.dispose();
    }
    _devicePoll?.cancel();
    super.dispose();
  }

  bool get _signInOnly => widget.definition.connect == 'codex_device';

  Future<void> _startChatgptSignIn() async {
    final t = AppL10n.of(context);
    setState(() {
      _connecting = true;
      _error = null;
    });
    try {
      final login = await widget.controller.api.chatgptLoginStart(accountId: widget.account?.id, billingMode: _billing);
      if (!mounted) return;
      setState(() => _deviceLogin = login);
      await launchUrl(Uri.parse('${login['verificationUrl']}'));
      final seconds = ((login['interval'] as num?) ?? 5).toInt().clamp(2, 30);
      _devicePoll = Timer.periodic(Duration(seconds: seconds), (_) => _pollChatgptSignIn());
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _connecting = false);
    }
  }

  bool _polling = false;

  Future<void> _pollChatgptSignIn() async {
    final login = _deviceLogin;
    if (login == null || _polling) return;
    _polling = true;
    final t = AppL10n.of(context);
    try {
      final result = await widget.controller.api.chatgptLoginPoll('${login['loginId']}');
      if (!mounted) return;
      if (result['status'] == 'done') {
        _devicePoll?.cancel();
        Navigator.of(context).pop(_editing ? t.saved : t.chatgptConnected);
      } else if (result['status'] == 'expired') {
        _devicePoll?.cancel();
        setState(() {
          _deviceLogin = null;
          _error = t.chatgptExpired;
        });
      }
    } on ApiException catch (error) {
      _devicePoll?.cancel();
      if (mounted) {
        setState(() {
          _deviceLogin = null;
          _error = error.message;
        });
      }
    } finally {
      _polling = false;
    }
  }

  Map<String, Object> _options() {
    final options = <String, Object>{
      for (final entry in _fields.entries)
        if (entry.value.text.trim().isNotEmpty) entry.key: entry.value.text.trim(),
    };
    if (_custom) {
      final manual = _manualModels.text.split(RegExp(r'[\n,]')).map((line) => line.trim()).where((line) => line.isNotEmpty).toList();
      if (manual.isNotEmpty) options['manualModels'] = manual;
      if (_authHeader.text.trim().isNotEmpty) options['authHeader'] = _authHeader.text.trim();
      if (_modelsPath.text.trim().isNotEmpty) options['modelsPath'] = _modelsPath.text.trim();
    }
    return options;
  }

  Map<String, String>? _headerMap() {
    if (!_custom) return null;
    final headers = <String, String>{};
    for (final line in _headers.text.split('\n')) {
      final index = line.indexOf(':');
      if (index > 0) headers[line.substring(0, index).trim()] = line.substring(index + 1).trim();
    }
    return headers;
  }

  Future<void> _runProbe() async {
    final t = AppL10n.of(context);
    setState(() {
      _probing = true;
      _error = null;
    });
    try {
      final result = await widget.controller.api.probeProvider(<String, Object?>{
        'definitionId': widget.definition.id,
        if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
        if (_custom) 'baseUrl': _baseUrl.text.trim(),
        'options': _options(),
        'headers': _headerMap(),
      });
      setState(() => _probe = result);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _probing = false);
    }
  }

  Future<void> _connectOpenRouter() async {
    setState(() {
      _connecting = true;
      _error = null;
    });
    try {
      final url = await widget.controller.api.startOpenRouterConnect(Uri.base.toString());
      await launchUrl(Uri.parse(url), webOnlyWindowName: '_self');
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _connecting = false);
    }
  }

  Future<void> _submit() async {
    final t = AppL10n.of(context);
    setState(() {
      _saving = true;
      _error = null;
    });
    final options = _options();
    final headers = _headerMap();
    try {
      if (_editing) {
        await widget.controller.api.updateAccount(widget.account!.id, <String, Object?>{
          'label': _label.text.trim(),
          'slug': _slug.text.trim(),
          if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
          if (widget.definition.editableBaseUrl) 'baseUrl': _baseUrl.text.trim(),
          'options': options,
          'headers': headers,
          'billingMode': _billing,
        });
        if (mounted) Navigator.of(context).pop(t.saved);
        return;
      }
      final result = await widget.controller.api.createAccount(<String, Object?>{
        'definitionId': widget.definition.id,
        // Unchanged defaults are left to the server, which numbers a second
        // account of the same provider ("Groq 2", groq-2).
        if (_label.text.trim() != widget.definition.label) 'label': _label.text.trim(),
        if (_slug.text.trim() != widget.definition.id.replaceAll('_', '-')) 'slug': _slug.text.trim(),
        if (_secret.text.trim().isNotEmpty) 'secret': _secret.text.trim(),
        if (widget.definition.editableBaseUrl) 'baseUrl': _baseUrl.text.trim(),
        if (options.isNotEmpty) 'options': options,
        'headers': headers,
        'billingMode': _billing,
      });
      final validation = Map<String, dynamic>.from(result['validation'] as Map);
      final sync = result['sync'] as Map?;
      final message = validation['status'] == 'healthy' && sync?['models'] != null
          ? t.providerAdded('${sync!['models']}')
          : t.providerAddedUnverified('${validation['message'] ?? sync?['error'] ?? ''}');
      if (mounted) Navigator.of(context).pop(message);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final definition = widget.definition;
    const gap = SizedBox(height: AppSpacing.sm);
    final (costLabel, costTone, costIcon) = providerCostTag(t, definition);
    return DialogFrame(
      maxWidth: 560,
      leading: ProviderAvatar(label: definition.label, size: 40),
      title: _editing ? widget.account!.label : definition.label,
      subtitle: Wrap(
        spacing: 6,
        runSpacing: 6,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: <Widget>[
          MetaPill(label: costLabel, tone: costTone, icon: costIcon),
          MetaPill(label: categoryLabel(t, definition.category)),
          if (definition.website != null)
            TextButton.icon(
              style: TextButton.styleFrom(visualDensity: VisualDensity.compact, padding: const EdgeInsets.symmetric(horizontal: 6)),
              onPressed: () => launchUrl(Uri.parse(definition.website!)),
              icon: const Icon(Icons.open_in_new_rounded, size: 14),
              label: Text(t.providerWebsite),
            ),
        ],
      ),
      body: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (definition.description.isNotEmpty) Text(definition.description, style: TextStyle(color: palette.textSoft, height: 1.45)),
          if (definition.category != 'custom' && definition.category != 'local') ...<Widget>[const SizedBox(height: AppSpacing.md), _CostSummary(definition: definition)],
          if (definition.id == 'openrouter' && !_editing) ...<Widget>[
            const SizedBox(height: AppSpacing.md),
            FilledButton.icon(
              onPressed: _connecting ? null : _connectOpenRouter,
              icon: _connecting ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.login_rounded, size: 18),
              label: Text(t.connectOpenRouter),
            ),
            const SizedBox(height: 6),
            Text(t.connectOpenRouterHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
            const SizedBox(height: AppSpacing.md),
            Row(
              children: <Widget>[
                Expanded(child: Divider(color: palette.border)),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 10),
                  child: Text(t.orManualKey, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                ),
                Expanded(child: Divider(color: palette.border)),
              ],
            ),
          ],
          if (_signInOnly) ...<Widget>[
            const SizedBox(height: AppSpacing.md),
            if (_deviceLogin == null) ...<Widget>[
              FilledButton.icon(
                onPressed: _connecting ? null : _startChatgptSignIn,
                icon: _connecting ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.login_rounded, size: 18),
                label: Text(_editing ? t.chatgptSignInAgain : t.chatgptSignIn),
              ),
              const SizedBox(height: 6),
              Text(t.chatgptSignInHint, style: TextStyle(color: palette.textMuted, fontSize: 12.5, height: 1.4)),
            ] else
              AppCard(
                padding: const EdgeInsets.all(16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(t.chatgptEnterCode, style: TextStyle(color: palette.textSoft, fontSize: 13)),
                    const SizedBox(height: 6),
                    Row(
                      children: <Widget>[
                        SelectableText('${_deviceLogin!['userCode']}', style: GoogleFonts.geistMono(fontSize: 24, fontWeight: FontWeight.w600, letterSpacing: 2)),
                        IconButton(
                          tooltip: t.copy,
                          onPressed: () => Clipboard.setData(ClipboardData(text: '${_deviceLogin!['userCode']}')),
                          icon: const Icon(Icons.copy_rounded, size: 18),
                        ),
                      ],
                    ),
                    TextButton.icon(
                      onPressed: () => launchUrl(Uri.parse('${_deviceLogin!['verificationUrl']}')),
                      icon: const Icon(Icons.open_in_new_rounded, size: 15),
                      label: Text(t.chatgptOpenPage),
                    ),
                    Row(
                      children: <Widget>[
                        const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)),
                        const SizedBox(width: 10),
                        Text(t.chatgptWaiting, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                      ],
                    ),
                  ],
                ),
              ),
          ],
          if (!_signInOnly) ...<Widget>[
            const SizedBox(height: AppSpacing.lg),
            Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: <Widget>[
                Expanded(
                  child: TextField(
                    controller: _secret,
                    obscureText: true,
                    autofocus: !_editing && definition.id != 'openrouter',
                    decoration: InputDecoration(
                      labelText: _editing
                          ? t.apiKeyKeep
                          : definition.keyOptional
                          ? t.apiKeyOptional
                          : t.apiKey,
                    ),
                  ),
                ),
                if (definition.keyUrl != null) ...<Widget>[
                  const SizedBox(width: 10),
                  OutlinedButton.icon(onPressed: () => launchUrl(Uri.parse(definition.keyUrl!)), icon: const Icon(Icons.key_rounded, size: 16), label: Text(t.providerGetKey)),
                ],
              ],
            ),
          ],
          for (final field in definition.fields) ...<Widget>[
            gap,
            TextField(
              controller: _fields[field.key],
              decoration: InputDecoration(labelText: field.label, hintText: field.placeholder),
            ),
          ],
          if (_custom) ...<Widget>[
            gap,
            TextField(
              controller: _baseUrl,
              decoration: InputDecoration(labelText: t.baseUrl, hintText: 'https://…/v1'),
            ),
          ],
          const SizedBox(height: AppSpacing.lg),
          Text(
            t.billingMode,
            style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
          ),
          const SizedBox(height: AppSpacing.xs),
          for (final (mode, title, hint) in <(String, String, String)>[
            // A full free tier, or just free models (declared or found in the listing).
            if (definition.freeTierAvailable && !definition.freeTierPartial)
              ('free_tier', t.billingOptionFree, t.billingOptionFreeHint)
            else if (definition.freeTierAvailable || (widget.account?.freeAvailable ?? false))
              ('free_tier', t.billingOptionFreeModels, t.billingOptionFreeModelsHint),
            if (definition.planAvailable) ('plan', t.billingOptionPlan, definition.planNote ?? t.billingOptionPlanHint),
            if (definition.planAvailable) ('plan_paid', t.billingOptionPlanPaid, t.billingOptionPlanPaidHint),
            ('paid', t.billingOptionPaid, t.billingOptionPaidHint),
          ])
            OptionCard(title: title, description: hint, selected: _billing == mode, onTap: () => setState(() => _billing = mode)),
          if (_billing == 'free_tier' && definition.freeTierAvailable && !definition.freeTierPartial) ...<Widget>[
            const SizedBox(height: AppSpacing.xs),
            Text(t.billingFreeWarning, style: TextStyle(color: palette.textSoft, fontSize: 12.5, height: 1.45)),
          ] else if (definition.hasAlwaysFreeModels) ...<Widget>[
            const SizedBox(height: AppSpacing.xs),
            Text(t.billingAlwaysFreeNote, style: TextStyle(color: palette.textSoft, fontSize: 12.5, height: 1.45)),
          ],
          const SizedBox(height: AppSpacing.md),
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton.icon(
              onPressed: () => setState(() => _advanced = !_advanced),
              icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
              label: Text(t.advancedOptions),
            ),
          ),
          if (_advanced) ...<Widget>[
            TextField(
              controller: _label,
              decoration: InputDecoration(labelText: t.displayNameLabel),
            ),
            gap,
            TextField(
              controller: _slug,
              onChanged: (_) => setState(() {}),
              decoration: InputDecoration(labelText: t.slugLabel, helperText: t.slugHint(_slug.text.isEmpty ? '…' : _slug.text)),
            ),
            if (_custom) ...<Widget>[
              gap,
              TextField(
                controller: _authHeader,
                decoration: InputDecoration(labelText: t.authHeaderLabel, hintText: 'x-api-key'),
              ),
              gap,
              TextField(
                controller: _headers,
                minLines: 1,
                maxLines: 4,
                decoration: InputDecoration(labelText: t.customHeaders, hintText: 'X-Org: my-team'),
              ),
              gap,
              TextField(
                controller: _modelsPath,
                decoration: InputDecoration(labelText: t.modelsPathLabel, hintText: '/models'),
              ),
              gap,
              TextField(
                controller: _manualModels,
                minLines: 2,
                maxLines: 6,
                decoration: InputDecoration(labelText: t.manualModels, helperText: t.manualModelsHint, helperMaxLines: 2),
              ),
            ],
          ],
          if (_custom) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            Align(
              alignment: Alignment.centerLeft,
              child: OutlinedButton.icon(
                onPressed: _probing ? null : _runProbe,
                icon: _probing ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2)) : const Icon(Icons.wifi_tethering_rounded, size: 16),
                label: Text(t.testConnection),
              ),
            ),
            if (_probe != null) _ProbeResult(result: _probe!),
          ],
          if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
        ],
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        if (!_signInOnly || _editing)
          FilledButton(
            onPressed: _saving ? null : _submit,
            child: _saving ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) : Text(_editing ? t.save : t.checkAndAdd),
          ),
      ],
    );
  }
}

class _ProbeResult extends StatelessWidget {
  const _ProbeResult({required this.result});

  final Map<String, dynamic> result;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final validation = Map<String, dynamic>.from((result['validation'] as Map?) ?? <String, Object>{});
    final models = ((result['models'] as List?) ?? const <Object>[]).cast<Map>();
    final status = validation['status'] as String? ?? 'unknown';
    final ok = status == 'healthy' && result['modelsError'] == null;
    final message = ok
        ? t.probeOk('${models.length}')
        : result['modelsError'] != null
        ? t.modelsFetchFailed('${result['modelsError']}')
        : '${validation['message'] ?? t.statusError}';
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          ok ? InlineSuccess(message: message) : InlineError(message: message),
          if (models.isNotEmpty) ...<Widget>[
            const SizedBox(height: AppSpacing.xs),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 120),
              child: SingleChildScrollView(
                child: Text(
                  models.take(120).map((model) => '${model['id']}').join(', '),
                  style: GoogleFonts.geistMono(fontSize: 12, color: palette.textMuted, height: 1.5),
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}

String metricLabel(AppL10n t, String metric) => switch (metric) {
  'rpm' => t.metricRpm,
  'rpd' => t.metricRpd,
  'tpm' => t.metricTpm,
  'tpd' => t.metricTpd,
  'balance' || 'credits_usd' => t.balance,
  'plan_requests' => t.metricPlanRequests,
  'plan_5h' => t.metricPlan5h,
  'plan_week' => t.metricPlanWeek,
  'plan_month' => t.metricPlanMonth,
  _ => metric,
};

/// How much of each provider's allowance is used, with reset times.
class _LimitsView extends StatefulWidget {
  const _LimitsView({required this.controller});

  final NeoRouterController controller;

  @override
  State<_LimitsView> createState() => _LimitsViewState();
}

class _LimitsViewState extends State<_LimitsView> {
  List<Map<String, dynamic>> _accounts = <Map<String, dynamic>>[];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final accounts = await widget.controller.api.quotas();
      setState(() => _accounts = accounts);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _edit(Map<String, dynamic> group) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (context) => _LimitsDialog(controller: widget.controller, account: Map<String, dynamic>.from(group['account'] as Map)),
    );
    if (saved == true) await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return AsyncBody(
      loading: _loading,
      error: _error,
      onRetry: _load,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Text(t.limitsDescription, style: TextStyle(color: neoRouterPaletteOf(context).textSoft, fontSize: 13.5, height: 1.45)),
          const SizedBox(height: AppSpacing.lg),
          for (final group in _accounts) ...<Widget>[
            _AccountLimits(group: group, canEdit: widget.controller.can('providers.manage'), onEdit: () => _edit(group)),
            const SizedBox(height: 28),
          ],
        ],
      ),
    );
  }
}

/// Daily metrics matter to people; per-minute windows reset before anyone
/// could act on them and the router already avoids them.
const Set<String> _dailyMetrics = <String>{'rpd', 'tpd'};
const double _lowFraction = 0.8;

double _usedFraction(Map window) {
  final limit = (window['limit'] as num?)?.toDouble();
  if (limit == null || limit <= 0) return 0;
  final remaining = (window['remaining'] as num?)?.toDouble();
  final used = remaining == null ? (window['used'] as num? ?? 0).toDouble() : limit - remaining;
  return (used / limit).clamp(0, 1).toDouble();
}

/// Windows worth showing: daily ones, and any that is used up right now.
bool _worthShowing(Map window) => _dailyMetrics.contains('${window['metric']}') || _usedFraction(window) >= 1;

class _AccountLimits extends StatelessWidget {
  const _AccountLimits({required this.group, required this.canEdit, required this.onEdit});

  final Map<String, dynamic> group;
  final bool canEdit;
  final VoidCallback onEdit;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final account = Map<String, dynamic>.from(group['account'] as Map);
    final pool = (group['pool'] as List).cast<Map>().where(_worthShowing).toList();
    final probes = (group['probes'] as List).cast<Map>();
    final models = (group['models'] as List).cast<Map>();
    // The fullest window of each model decides whether it is running low.
    final low = <({Map model, Map window, double fraction})>[];
    for (final model in models) {
      final windows = (model['windows'] as List).cast<Map>().where(_worthShowing).toList();
      if (windows.isEmpty) continue;
      windows.sort((a, b) => _usedFraction(b).compareTo(_usedFraction(a)));
      final fraction = _usedFraction(windows.first);
      if (fraction >= _lowFraction) low.add((model: model, window: windows.first, fraction: fraction));
    }
    low.sort((a, b) => b.fraction.compareTo(a.fraction));
    final limits = (group['limits'] as List? ?? const <dynamic>[]).cast<Map>();
    final rows = <Widget>[
      for (final limit in limits) _LimitRow(limit: limit),
      // Plan windows get a usage bar; other probes (balances) stay plain values.
      for (final probe in probes.where((item) => '${item['metric']}'.startsWith('plan_') && item['limit'] != null))
        _WindowRow(title: metricLabel(t, '${probe['metric']}'), window: probe),
      for (final probe in probes.where((item) => !'${item['metric']}'.startsWith('plan_') || item['limit'] == null))
        SettingsRow(
          label: metricLabel(t, '${probe['metric']}'),
          control: Text('${probe['remaining'] ?? '—'}${probe['limit'] == null ? '' : ' / ${probe['limit']}'}', style: TextStyle(color: palette.text)),
        ),
      if (group['concurrency'] is Map)
        SettingsRow(
          label: t.limitConcurrent,
          control: Text(
            (group['concurrency'] as Map)['source'] == 'learned'
                ? t.limitConcurrentValue('${(group['concurrency'] as Map)['limit']}')
                : t.limitConcurrentDocumented('${(group['concurrency'] as Map)['limit']}'),
            style: TextStyle(color: palette.textSoft, fontSize: 13),
          ),
        ),
      for (final window in pool) _WindowRow(title: metricLabel(t, '${window['metric']}'), window: window),
      if (models.isNotEmpty)
        SettingsRow(
          label: t.limitsModelsRoom('${models.length - low.where((item) => item.fraction >= 1).length}', '${models.length}'),
          control: low.isEmpty ? Icon(Icons.check_rounded, size: 18, color: palette.success) : const SizedBox.shrink(),
        ),
      for (final item in low.take(6))
        _WindowRow(title: '${shortModelName('${item.model['id']}')} · ${metricLabel(t, '${item.window['metric']}')}', window: item.window),
      _ModelLimits(models: models),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(
          title: '${account['label']}',
          trailing: canEdit ? TextButton.icon(onPressed: onEdit, icon: const Icon(Icons.tune_rounded, size: 17), label: Text(t.limitsEdit)) : null,
        ),
        if (rows.isEmpty)
          AppCard(
            child: Text(t.noQuotaData, style: TextStyle(color: palette.textSoft)),
          )
        else
          SettingsCardList(children: rows),
      ],
    );
  }
}

String _limitSource(AppL10n t, String source) => switch (source) {
  'manual' => t.limitSourceManual,
  'header' => t.limitSourceHeader,
  'learned' => t.limitSourceLearned,
  _ => t.limitSourceCurated,
};

String _limitRange(Map perModel) {
  final low = (perModel['min'] as num).toInt();
  final high = (perModel['max'] as num).toInt();
  return low == high ? formatCount(low) : '${formatCount(low)}–${formatCount(high)}';
}

/// One kind of limit of an account: what all its models share and what each
/// model gets, with where the number comes from.
class _LimitRow extends StatelessWidget {
  const _LimitRow({required this.limit});

  final Map limit;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final shared = limit['shared'] as Map?;
    final perModel = limit['perModel'] as Map?;
    final sources = <String>{
      if (shared != null) '${shared['source']}',
      if (perModel != null) ...(perModel['sources'] as List).map((item) => '$item'),
    };
    final values = <String>[
      if (shared != null) t.limitSharedValue(formatCount((shared['limit'] as num).toInt())),
      if (perModel != null) t.limitPerModelValue(_limitRange(perModel)),
    ];
    return SettingsRow(
      label: metricLabel(t, '${limit['metric']}'),
      description: sources.map((source) => _limitSource(t, source)).join(' · '),
      control: Column(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: <Widget>[
          for (final value in values) Text(value, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: palette.text)),
        ],
      ),
    );
  }
}

String _modelLimitSummary(AppL10n t, Map model) {
  final byMetric = <String, num>{};
  for (final window in (model['windows'] as List).cast<Map>()) {
    final value = window['limit'];
    if (value is num && value > 0 && window['source'] != 'error') byMetric['${window['metric']}'] = value;
  }
  String part(String metric, String Function(String) label) => byMetric.containsKey(metric) ? label(formatCount(byMetric[metric]!.toInt())) : '';
  return <String>[
    part('rpm', t.limitShortRpm),
    part('rpd', t.limitShortRpd),
    part('tpm', t.limitShortTpm),
    part('tpd', t.limitShortTpd),
  ].where((item) => item.isNotEmpty).join(' · ');
}

/// Every model's own limits, folded away until someone wants them.
class _ModelLimits extends StatelessWidget {
  const _ModelLimits({required this.models});

  final List<Map> models;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final rows = <({String name, String summary})>[
      for (final model in models)
        if (_modelLimitSummary(t, model).isNotEmpty) (name: shortModelName('${model['id']}'), summary: _modelLimitSummary(t, model)),
    ]..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    if (rows.isEmpty) return const SizedBox.shrink();
    return Theme(
      data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
      child: ExpansionTile(
        tilePadding: const EdgeInsets.symmetric(horizontal: 20),
        childrenPadding: const EdgeInsets.fromLTRB(20, 0, 20, 12),
        title: Text(t.limitsPerModelTitle('${rows.length}'), style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: palette.text)),
        children: <Widget>[
          for (final row in rows)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 5),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(flex: 5, child: Text(row.name, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, color: palette.text))),
                  const SizedBox(width: 12),
                  Expanded(flex: 6, child: Text(row.summary, textAlign: TextAlign.end, style: TextStyle(fontSize: 12.5, color: palette.textSoft))),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

const List<String> _statedMetrics = <String>['rpm', 'rpd', 'tpm', 'tpd', 'concurrent'];

/// States what an account can take. A number wins over everything NeoRouter
/// reads or learns; an empty field gives the decision back to NeoRouter.
class _LimitsDialog extends StatefulWidget {
  const _LimitsDialog({required this.controller, required this.account});

  final NeoRouterController controller;
  final Map<String, dynamic> account;

  @override
  State<_LimitsDialog> createState() => _LimitsDialogState();
}

class _LimitsDialogState extends State<_LimitsDialog> {
  late final Map<String, TextEditingController> _fields;
  late final Map<String, num> _before;
  String? _error;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _before = <String, num>{
      for (final entry in ((widget.account['limits'] as Map?) ?? const <String, Object?>{}).entries) '${entry.key}': entry.value as num,
    };
    _fields = <String, TextEditingController>{
      for (final metric in _statedMetrics) metric: TextEditingController(text: _before[metric]?.toInt().toString() ?? ''),
    };
  }

  @override
  void dispose() {
    for (final field in _fields.values) {
      field.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    final t = AppL10n.of(context);
    final body = <String, int?>{};
    for (final metric in _statedMetrics) {
      final text = _fields[metric]!.text.trim().replaceAll(RegExp(r'[ _,.]'), '');
      if (text.isEmpty) {
        if (_before.containsKey(metric)) body[metric] = null;
        continue;
      }
      final value = int.tryParse(text);
      if (value == null || value < 1) {
        setState(() => _error = t.limitsInvalid);
        return;
      }
      if (_before[metric]?.toInt() != value) body[metric] = value;
    }
    if (body.isEmpty) {
      Navigator.of(context).pop(false);
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await widget.controller.api.setAccountLimits('${widget.account['id']}', body);
      if (mounted) Navigator.of(context).pop(true);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } catch (_) {
      setState(() => _error = t.serverUnreachable);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final labels = <String, String>{
      'rpm': t.metricRpm,
      'rpd': t.metricRpd,
      'tpm': t.metricTpm,
      'tpd': t.metricTpd,
      'concurrent': t.limitConcurrent,
    };
    return AlertDialog(
      title: Text(t.limitsEditTitle('${widget.account['label']}')),
      content: SizedBox(
        width: 420,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(t.limitsEditHint, style: TextStyle(color: palette.textSoft, fontSize: 13, height: 1.45)),
              const SizedBox(height: AppSpacing.md),
              for (final metric in _statedMetrics) ...<Widget>[
                TextField(
                  controller: _fields[metric],
                  keyboardType: TextInputType.number,
                  inputFormatters: <TextInputFormatter>[FilteringTextInputFormatter.allow(RegExp(r'[0-9 _,.]'))],
                  decoration: InputDecoration(labelText: labels[metric], hintText: t.limitAutomatic),
                ),
                const SizedBox(height: AppSpacing.sm),
              ],
              if (_error != null) Text(_error!, style: TextStyle(color: palette.error, fontSize: 13)),
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(false), child: Text(t.cancel)),
        FilledButton(onPressed: _saving ? null : _save, child: Text(t.save)),
      ],
    );
  }
}

class _WindowRow extends StatelessWidget {
  const _WindowRow({required this.title, required this.window});

  final String title;
  final Map window;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final limit = (window['limit'] as num?)?.toDouble();
    final remaining = (window['remaining'] as num?)?.toDouble() ?? 0;
    final used = limit == null ? (window['used'] as num? ?? 0).toDouble() : (limit - remaining).clamp(0, limit).toDouble();
    final resetAt = window['resetAt'] as num?;
    final detail = <String>[
      limit == null ? formatCount(used) : t.usedOf(formatCount(used), formatCount(limit)),
      if (resetAt != null) t.limitResets(formatTime(DateTime.fromMillisecondsSinceEpoch(resetAt.toInt()))),
    ].join(' · ');
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13.5, color: palette.text),
                ),
              ),
              Text(detail, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
            ],
          ),
          const SizedBox(height: 8),
          UsageBar(fraction: limit == null || limit == 0 ? 0 : used / limit, height: 4),
        ],
      ),
    );
  }
}
