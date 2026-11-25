import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_selection.dart';
import 'main_key_expiry.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_pool.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

/// "Free · Plans · Paid" for the kinds of models a key may use.
String keyKindsLabel(AppL10n t, RouterKey key) =>
    <String>[if (key.allowFree) t.kindFreeShort, if (key.allowPlan) t.kindPlanShort, if (key.allowPaid) t.kindPaidShort, if (key.usesPool) t.keyPoolChip].join(' · ');

class KeysScreen extends StatefulWidget {
  const KeysScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<KeysScreen> createState() => _KeysScreenState();
}

class _KeysScreenState extends State<KeysScreen> {
  List<RouterKey> _keys = <RouterKey>[];
  List<RoutingProfile> _profiles = <RoutingProfile>[];
  bool _loading = true;
  String? _error;
  bool _showRevoked = false;
  bool _showExpired = false;
  Timer? _expiryRefresh;

  @override
  void initState() {
    super.initState();
    _load();
    _expiryRefresh = Timer.periodic(const Duration(minutes: 1), (_) { if (mounted) setState(() {}); });
  }

  @override
  void dispose() {
    _expiryRefresh?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    if (!mounted) return;
    setState(() => _error = null);
    try {
      final results = await Future.wait(<Future<Object>>[widget.controller.api.keys(), widget.controller.api.profiles()]);
      if (!mounted) return;
      setState(() {
        _keys = results[0] as List<RouterKey>;
        _profiles = results[1] as List<RoutingProfile>;
      });
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _open([RouterKey? key]) async {
    final created = await showDialog<({RouterKey key, String secret})>(
      context: context,
      builder: (_) => _KeyForm(controller: widget.controller, profiles: _profiles, existing: key),
    );
    if (!mounted) return;
    await _load();
    if (created != null && mounted) await _showSecret(created.secret, created.key.expiresAt);
  }

  Future<void> _showSecret(String secret, String? expiresAt) async {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    await showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (dialogContext) => AlertDialog(
        title: Text(t.keySecretTitle),
        content: SizedBox(
          width: 480,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              KeyExpiryStatus(expiresAt: expiresAt == null ? null : DateTime.tryParse(expiresAt)),
              const SizedBox(height: AppSpacing.sm),
              Text(t.keySecretBody, style: TextStyle(color: palette.textSoft, height: 1.45)),
              const SizedBox(height: AppSpacing.md),
              Container(
                padding: const EdgeInsets.fromLTRB(14, 12, 4, 12),
                decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(AppRadius.control)),
                child: Row(
                  children: <Widget>[
                    Expanded(
                      child: SelectableText(secret, style: GoogleFonts.geistMono(fontSize: 13, color: palette.text)),
                    ),
                    IconButton(
                      tooltip: t.copy,
                      icon: const Icon(Icons.copy_rounded, size: 17),
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: secret));
                        showSnack(context, t.copied);
                      },
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        actions: <Widget>[FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.keySecretDone))],
      ),
    );
  }

  Future<void> _revoke(RouterKey key) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.revoke, message: t.revokeConfirm(key.name), confirmLabel: t.revoke, cancelLabel: t.cancel);
    if (!confirmed) return;
    await widget.controller.api.revokeKey(key.id);
    await _load();
  }

  Future<void> _delete(RouterKey key) async {
    final t = AppL10n.of(context);
    final confirmed = await confirmAction(context, title: t.delete, message: t.deleteKeyConfirm(key.name), confirmLabel: t.delete, cancelLabel: t.cancel);
    if (!confirmed) return;
    await widget.controller.api.deleteKey(key.id);
    await _load();
  }

  Future<void> _setPriority(RouterKey key, String priority) async {
    try {
      final updated = await widget.controller.api.updateKey(key.id, <String, Object?>{'priority': priority});
      if (mounted) setState(() => _keys = <RouterKey>[for (final item in _keys) item.id == key.id ? updated : item]);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final active = _keys.where((key) => !key.revoked && !key.expired && !key.isConnectedApp).toList();
    final apps = _keys.where((key) => !key.revoked && !key.expired && key.isConnectedApp).toList();
    final expired = _keys.where((key) => !key.revoked && key.expired).toList();
    final revoked = _keys.where((key) => key.revoked).toList();
    final controller = widget.controller;
    final canCreate = controller.can('keys.create');
    Widget row(RouterKey key) => _KeyRow(
      routerKey: key,
      profiles: _profiles,
      showCreator: controller.showCreators,
      canEdit: controller.can('keys.edit'),
      canDelete: controller.can('keys.delete'),
      onAction: (action) => switch (action) {
        'edit' => _open(key),
        'revoke' => _revoke(key),
        _ => _delete(key),
      },
      onPriority: (priority) => _setPriority(key, priority),
    );
    return PageScaffold(
      header: ScreenHeader(
        title: t.keysTitle,
        description: t.keysDescription,
        trailing: canCreate
            ? FilledButton.icon(onPressed: _loading ? null : () => _open(), icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.createKey))
            : null,
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (apps.isNotEmpty) ...<Widget>[
                SettingsHeading(title: t.connectedAppsTitle, description: t.connectedAppsHint),
                SettingsCardList(children: <Widget>[for (final key in apps) row(key)]),
                const SizedBox(height: AppSpacing.lg),
                if (active.isNotEmpty) SettingsHeading(title: t.keysTitle),
              ],
              if (active.isEmpty && apps.isNotEmpty)
                const SizedBox.shrink()
              else if (active.isEmpty)
                AppCard(
                  child: EmptyState(
                    icon: Icons.key_outlined,
                    title: expired.isNotEmpty ? t.noActiveKeysTitle : t.noKeysTitle,
                    message: expired.isNotEmpty ? t.noActiveKeysHint : t.noKeysMessage,
                    action: canCreate
                        ? FilledButton.icon(onPressed: () => _open(), icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.createKey))
                        : null,
                  ),
                )
              else
                SettingsCardList(children: <Widget>[for (final key in active) row(key)]),
              if (expired.isNotEmpty) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                Align(alignment: Alignment.centerLeft, child: TextButton.icon(
                  onPressed: () => setState(() => _showExpired = !_showExpired),
                  icon: Icon(_showExpired ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.expiredKeys('${expired.length}')),
                )),
                if (_showExpired) SettingsCardList(children: <Widget>[for (final key in expired) row(key)]),
              ],
              if (revoked.isNotEmpty) ...<Widget>[
                const SizedBox(height: AppSpacing.md),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: () => setState(() => _showRevoked = !_showRevoked),
                    icon: Icon(_showRevoked ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                    label: Text(t.revokedKeys('${revoked.length}')),
                  ),
                ),
                if (_showRevoked) Opacity(opacity: 0.7, child: SettingsCardList(children: <Widget>[for (final key in revoked) row(key)])),
              ],
              const SizedBox(height: AppSpacing.lg),
              Text(t.keysFooter, style: TextStyle(color: palette.textMuted, fontSize: 13, height: 1.45)),
            ],
          ),
        ),
      ],
    );
  }
}

class _KeyRow extends StatelessWidget {
  const _KeyRow({
    required this.routerKey,
    required this.profiles,
    required this.onAction,
    required this.onPriority,
    this.showCreator = false,
    this.canEdit = true,
    this.canDelete = true,
  });

  final RouterKey routerKey;
  final List<RoutingProfile> profiles;
  final ValueChanged<String> onAction;
  final ValueChanged<String> onPriority;
  final bool showCreator;
  final bool canEdit;
  final bool canDelete;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final key = routerKey;
    final budget = key.budgetUsd;
    final used = key.lastUsedAt == null ? t.keyNeverUsed : t.keyUsed(formatAgo(t, DateTime.parse(key.lastUsedAt!).toLocal()));
    final subtitle = <String>[
      if (key.revoked) t.revoked else keyKindsLabel(t, key),
      if (key.isConnectedApp) key.appOrigin == null ? t.connectedApp : t.connectedVia(key.appOrigin!),
      used, if (showCreator) createdByLabel(t, key.createdBy)].join(' · ');
    final actions = <PopupMenuEntry<String>>[
      if (!key.revoked && canEdit) PopupMenuItem<String>(value: 'edit', child: Text(key.expired ? t.keyRenewEdit : t.edit)),
      if (!key.revoked && canEdit) PopupMenuItem<String>(value: 'revoke', child: Text(t.revoke)),
      if (canDelete)
        PopupMenuItem<String>(
          value: 'delete',
          child: Text(t.delete, style: TextStyle(color: palette.error)),
        ),
    ];
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 16, 8, 16),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Container(
            width: 38,
            height: 38,
            decoration: BoxDecoration(color: palette.surfaceRaised, borderRadius: BorderRadius.circular(10)),
            child: Icon(key.isConnectedApp ? Icons.apps_rounded : Icons.key_outlined, size: 18, color: palette.textSoft),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Row(
                  children: <Widget>[
                    Flexible(
                      child: Text(
                        key.name,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: palette.text),
                      ),
                    ),
                    // On a phone the name needs the room.
                    if (MediaQuery.sizeOf(context).width >= 600) ...<Widget>[
                      const SizedBox(width: 10),
                      Text('${key.prefix}…', style: GoogleFonts.geistMono(fontSize: 12, color: palette.textMuted)),
                    ],
                  ],
                ),
                const SizedBox(height: 3),
                Text(
                  subtitle,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13, color: palette.textMuted),
                ),
                if (!key.revoked) ...<Widget>[
                  const SizedBox(height: 8),
                  KeyExpiryStatus(expiresAt: key.expiresAt == null ? null : DateTime.tryParse(key.expiresAt!), expired: key.expired),
                ],
                if (key.tokenLimit != null && !key.revoked && !key.expired) ...<Widget>[
                  const SizedBox(height: 10),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 360),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: <Widget>[
                        Text(
                          <String>[
                            t.tokensUsedOf(formatCount(key.tokensUsed ?? 0), formatCount(key.tokenLimit!)),
                            if (key.tokenResetsAt != null) t.resetsAt(formatTime(DateTime.parse(key.tokenResetsAt!).toLocal())),
                          ].join(' · '),
                          style: TextStyle(fontSize: 12.5, color: palette.textSoft),
                        ),
                        const SizedBox(height: 6),
                        UsageBar(fraction: key.tokenLimit == 0 ? 1 : (key.tokensUsed ?? 0) / key.tokenLimit!, height: 4),
                      ],
                    ),
                  ),
                ],
                if (budget != null && !key.revoked && !key.expired) ...<Widget>[
                  const SizedBox(height: 10),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 360),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: <Widget>[
                        Text(
                          <String>[
                            t.spentOf(formatUsd(key.spentUsd), formatUsd(budget)),
                            if (key.periodResetsAt != null) t.resetsAt(formatTime(DateTime.parse(key.periodResetsAt!).toLocal())),
                          ].join(' · '),
                          style: TextStyle(fontSize: 12.5, color: palette.textSoft),
                        ),
                        const SizedBox(height: 6),
                        UsageBar(fraction: budget == 0 ? 1 : key.spentUsd / budget, height: 4),
                        if (_paceText(t, key) != null) ...<Widget>[
                          const SizedBox(height: 6),
                          Text(
                            _paceText(t, key)!,
                            style: TextStyle(fontSize: 12, color: key.paceState == 'tight' || key.paceState == 'over' ? palette.warning : palette.textMuted),
                          ),
                        ],
                      ],
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (!key.revoked && !key.expired) _PriorityPicker(priority: key.priority, onChanged: canEdit ? onPriority : null),
          if (actions.isNotEmpty)
            PopupMenuButton<String>(
              tooltip: t.moreActions,
              icon: const Icon(Icons.more_horiz_rounded, size: 20),
              onSelected: onAction,
              itemBuilder: (_) => actions,
            ),
        ],
      ),
    );
  }
}

/// A key's priority on its row: a quiet label that opens the three choices.
class _PriorityPicker extends StatelessWidget {
  const _PriorityPicker({required this.priority, required this.onChanged});

  final String priority;
  final ValueChanged<String>? onChanged;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final narrow = MediaQuery.sizeOf(context).width < 600;
    final label = narrow
        ? switch (priority) { 'high' => t.priorityHigh, 'low' => t.priorityLow, _ => t.priorityNormal }
        : switch (priority) { 'high' => t.priorityHighLabel, 'low' => t.priorityLowLabel, _ => t.priorityNormalLabel };
    final text = Text(
      label,
      style: TextStyle(
        fontSize: 13,
        fontWeight: priority == 'high' ? FontWeight.w600 : FontWeight.w400,
        color: priority == 'high' ? palette.text : palette.textMuted,
      ),
    );
    final padding = const EdgeInsets.symmetric(horizontal: 8, vertical: 10);
    if (onChanged == null) return Padding(padding: padding, child: text);
    return PopupMenuButton<String>(
      tooltip: t.priorityTitle,
      initialValue: priority,
      onSelected: (value) {
        if (value != priority) onChanged!(value);
      },
      itemBuilder: (_) => <PopupMenuEntry<String>>[
        PopupMenuItem<String>(
          enabled: false,
          height: 36,
          child: Text(t.priorityMenuHeading, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
        ),
        for (final (value, title, hint) in <(String, String, String)>[
          ('high', t.priorityHigh, t.priorityHighHint),
          ('normal', t.priorityNormal, t.priorityNormalHint),
          ('low', t.priorityLow, t.priorityLowHint),
        ])
          PopupMenuItem<String>(
            value: value,
            child: Row(
              children: <Widget>[
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      Text(title, style: TextStyle(fontSize: 14, fontWeight: FontWeight.w600, color: palette.text)),
                      Text(hint, style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                    ],
                  ),
                ),
                const SizedBox(width: 16),
                SizedBox(width: 18, child: value == priority ? Icon(Icons.check_rounded, size: 18, color: palette.accent) : null),
              ],
            ),
          ),
      ],
      child: Padding(
        padding: padding,
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            text,
            const SizedBox(width: 2),
            Icon(Icons.expand_more_rounded, size: 16, color: palette.textMuted),
          ],
        ),
      ),
    );
  }
}

class _KeyForm extends StatefulWidget {
  const _KeyForm({required this.controller, required this.profiles, this.existing});

  final NeoRouterController controller;
  final List<RoutingProfile> profiles;
  final RouterKey? existing;

  @override
  State<_KeyForm> createState() => _KeyFormState();
}

class _KeyFormState extends State<_KeyForm> {
  late final TextEditingController _name = TextEditingController(text: widget.existing?.name ?? '');
  late final TextEditingController _budget = TextEditingController(text: widget.existing?.budgetUsd?.toString() ?? '');
  late final TextEditingController _rpm = TextEditingController(text: widget.existing?.rpmLimit?.toString() ?? '');
  late final _concurrent = TextEditingController(text: widget.existing?.concurrentLimit?.toString() ?? '');
  late final TextEditingController _tokens = TextEditingController(text: widget.existing?.tokenLimit?.toString() ?? '');
  late final TextEditingController _maxPrice = TextEditingController(text: widget.existing?.maxPricePerMillion?.toString() ?? '');
  late String _tokenPeriod = widget.existing?.tokenPeriod ?? 'monthly';
  // Kinds of model ("kind:speech") have their own chips; the text keeps models and routings.
  late List<String> _allowed = widget.existing?.allowedModels.where((item) => !item.startsWith('kind:')).toList() ?? <String>[];
  late Set<String>? _kinds = () {
    final kinds = widget.existing?.allowedModels.where((item) => item.startsWith('kind:')).map((item) => item.substring(5)).toSet() ?? <String>{};
    return kinds.isEmpty ? null : kinds;
  }();
  List<String> _available = <String>[];
  late bool _free = widget.existing?.allowFree ?? true;
  late bool _plan = widget.existing?.allowPlan ?? true;
  late bool _paid = widget.existing?.allowPaid ?? false;
  late bool _pool = widget.existing?.usesPool ?? false;
  late String _period = widget.existing?.budgetPeriod ?? 'monthly';
  late String _routingGroup = widget.existing?.raw['routingGroup'] as String? ?? '';
  List<Map<String, dynamic>> _groups = [];
  late String _profile = widget.existing?.profile ?? 'balanced';
  late DateTime? _expiresAt = widget.existing?.expiresAt == null ? null : DateTime.tryParse(widget.existing!.expiresAt!);
  bool _expiryChanged = false;
  late bool _advanced = widget.existing?.rpmLimit != null || widget.existing?.concurrentLimit != null;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();

    widget.controller.api
        .routingGroups()
        .then((groups) {
          if (mounted) setState(() => _groups = groups);
        })
        .catchError((_) {});
    // The kinds of model this user has connected, for the kind chips.
    widget.controller.api
        .capabilities()
        .then((items) {
          if (mounted) setState(() => _available = items.where((item) => item.routable && item.total > 0 && item.id != 'live').map((item) => item.id).toList());
        })
        .catchError((_) {});
  }

  bool _kindOn(String kind) => _kinds == null || _kinds!.contains(kind);

  void _toggleKind(String kind, bool on) => setState(() {
    final next = <String>{...(_kinds ?? _available.toSet())};
    on ? next.add(kind) : next.remove(kind);
    _kinds = next;
  });

  // What gets saved: kind entries when kinds are narrowed or models chosen
  // (so a chosen chat model leaves other kinds open), then models and routings.
  List<String> _allowedList() {
    final lines = _allowed;
    final kinds = _kinds ?? _available.toSet();
    final narrowed = _kinds != null && !_available.every(kinds.contains);
    final hasModels = lines.any((line) => !RegExp(r'^(?:auto(?:$|[-:*?])|pool:)', caseSensitive: false).hasMatch(line));
    return <String>[
      if (narrowed || hasModels)
        for (final kind in kinds) 'kind:$kind',
      ...lines,
    ];
  }

  @override
  void dispose() {
    _name.dispose();
    _budget.dispose();
    _rpm.dispose();
    _concurrent.dispose();
    _tokens.dispose();
    _maxPrice.dispose();
    super.dispose();
  }

  // Routings offered next to the models in the picker.
  List<(String, String)> _routes(AppL10n t) => <(String, String)>[
    ('auto', t.autoKeyDefault),
    for (final profile in widget.profiles) ('auto:${profile.slug}', 'Auto (${profileLabel(t, profile)})'),
    for (final group in _groups) ('pool:${group['name']}', '${group['name']}'),
  ];


  Future<void> _submit() async {
    if (_expiryChanged && _expiresAt != null && !_expiresAt!.isAfter(DateTime.now())) {
      setState(() => _error = AppL10n.of(context).keyExpiryInvalid);
      return;
    }
    if (!_free && !_plan && !_paid) {
      setState(() => _error = AppL10n.of(context).pickOneKind);
      return;
    }
    if (_kinds != null && _kinds!.isEmpty) {
      setState(() => _error = AppL10n.of(context).keyKindsNone);
      return;
    }
    for (final field in [_rpm, _concurrent]) {
      final value = field.text.trim();
      if (value.isNotEmpty && (int.tryParse(value) ?? 0) < 1) {
        setState(() {
          _advanced = true;
          _error = AppL10n.of(context).limitsInvalid;
        });
        return;
      }
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    final body = <String, Object?>{
      'name': _name.text.trim(),
      'allowFree': _free,
      'allowPlan': _plan,
      'allowPaid': _paid,
      'budgetUsd': _paid ? double.tryParse(_budget.text.trim().replaceAll(',', '.')) : null,
      'budgetPeriod': _period,
      'rpmLimit': int.tryParse(_rpm.text.trim()),
      'concurrentLimit': int.tryParse(_concurrent.text.trim()),
      'maxPricePerMillion': _paid ? double.tryParse(_maxPrice.text.trim().replaceAll(',', '.')) : null,
      'tokenLimit': _free || _plan ? int.tryParse(_tokens.text.trim().replaceAll(RegExp(r'[^0-9]'), '')) : null,
      'tokenPeriod': _tokenPeriod,
      'allowedModels': _allowedList(),
      'profile': _profile,
      'routingGroup': _routingGroup.isEmpty ? null : _routingGroup,
      if (_expiryChanged) 'expiresAt': _expiresAt?.toUtc().toIso8601String(),
      // Only sent while in a pool, so a key keeps its choice if the pool is left and rejoined.
      if (widget.controller.inPool) 'poolAccess': _pool ? 'pool' : 'own',
    };
    try {
      if (widget.existing != null) {
        await widget.controller.api.updateKey(widget.existing!.id, body);
        if (mounted) Navigator.of(context).pop();
      } else {
        final created = await widget.controller.api.createKey(body);
        if (mounted) Navigator.of(context).pop(created);
      }
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final kinds = _available.length > 1
        ? Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              for (final kind in _available)
                FilterChip(
                  visualDensity: VisualDensity.compact,
                  avatar: Icon(capabilityInfo(kind).icon, size: 15, color: _kindOn(kind) ? palette.accent : palette.textMuted),
                  label: Text(capabilityInfo(kind).label(t), style: TextStyle(color: _kindOn(kind) ? palette.text : palette.textMuted)),
                  selected: _kindOn(kind),
                  showCheckmark: false,
                  selectedColor: palette.accentSoft,
                  side: BorderSide(color: _kindOn(kind) ? palette.accent.withValues(alpha: 0.6) : palette.border),
                  onSelected: (value) => _toggleKind(kind, value),
                ),
            ],
          )
        : null;
    return Dialog(
      insetPadding: const EdgeInsets.all(16),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560, maxHeight: 760),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: <Widget>[
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 20, 12, 8),
              child: Row(
                children: <Widget>[
                  Expanded(child: Text(widget.existing == null ? t.createKey : t.editKey, style: sectionTitleStyle(palette).copyWith(fontSize: 18))),
                  IconButton(tooltip: t.cancel, onPressed: _saving ? null : () => Navigator.of(context).pop(), icon: const Icon(Icons.close_rounded)),
                ],
              ),
            ),
            Flexible(
              child: SingleChildScrollView(
                padding: const EdgeInsets.fromLTRB(24, 4, 24, 16),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    TextField(
                      controller: _name,
                      autofocus: widget.existing == null,
                      decoration: InputDecoration(labelText: t.keyName, hintText: t.keyNameHint),
                    ),
                    const SizedBox(height: AppSpacing.md),
                    KeyExpiryField(
                      value: _expiresAt,
                      wasExpired: widget.existing?.expired ?? false,
                      enabled: !_saving,
                      onChanged: (value) => setState(() {
                        _expiresAt = value;
                        _expiryChanged = true;
                        _error = null;
                      }),
                    ),
                    _FormSection(
                      title: t.keySectionModels,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: <Widget>[
                          KeyAccessFields(
                            free: _free,
                            plan: _plan,
                            paid: _paid,
                            budget: _budget,
                            period: _period,
                            tokens: _tokens,
                            maxPrice: _maxPrice,
                            tokenPeriod: _tokenPeriod,
                            onFree: (value) => setState(() => _free = value),
                            onPlan: (value) => setState(() => _plan = value),
                            onPaid: (value) => setState(() => _paid = value),
                            onPeriod: (value) => setState(() => _period = value),
                            onTokenPeriod: (value) => setState(() => _tokenPeriod = value),
                            pool: _pool,
                            onPool: widget.controller.inPool ? (value) => setState(() => _pool = value) : null,
                            kinds: kinds,
                          ),
                          if (_kinds != null && _kinds!.isEmpty) ...<Widget>[const SizedBox(height: AppSpacing.xs), InlineError(message: t.keyKindsNone)],
                          const SizedBox(height: AppSpacing.md),
                          ModelSelectionField(
                            controller: widget.controller,
                            value: _allowed,
                            onChanged: (value) => setState(() => _allowed = value),
                            routes: _routes(t),
                            capabilities: _available.where(_kindOn).toList(),
                          ),
                        ],
                      ),
                    ),
                    _FormSection(
                      title: t.keySectionRouting,
                      trailing: TextButton.icon(
                        onPressed: () => setState(() => _advanced = !_advanced),
                        icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                        label: Text(t.advancedOptions),
                      ),
                      child: !_advanced
                          ? const SizedBox.shrink()
                          : Column(
                              crossAxisAlignment: CrossAxisAlignment.stretch,
                              children: <Widget>[
                                _FormSection(
                                  title: t.trafficLimitsTitle,
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.stretch,
                                    children: [
                                      Text(t.keyTrafficLimitsHint),
                                      const SizedBox(height: AppSpacing.sm),
                                      _LimitRow(label: t.keyRateLimit, controller: _rpm, unit: t.keyRequestsPerMinute),
                                      _LimitRow(label: t.limitConcurrent, controller: _concurrent, unit: ''),
                                    ],
                                  ),
                                ),
                                Row(
                                  children: <Widget>[
                                    Expanded(
                                      child: DropdownButtonFormField<String>(
                                        isExpanded: true,
                                        initialValue: widget.profiles.any((item) => item.id == _profile) ? _profile : 'balanced',
                                        decoration: InputDecoration(labelText: t.defaultProfile),
                                        items: <DropdownMenuItem<String>>[
                                          for (final profile in widget.profiles)
                                            DropdownMenuItem<String>(
                                              value: profile.id,
                                              child: Text(profile.id == 'balanced' ? t.presetBalancedDefault : profileLabel(t, profile), overflow: TextOverflow.ellipsis),
                                            ),
                                        ],
                                        onChanged: (value) => setState(() => _profile = value ?? _profile),
                                      ),
                                    ),
                                    const SizedBox(width: AppSpacing.sm),
                                    Expanded(
                                      child: DropdownButtonFormField<String>(
                                        isExpanded: true,
                                        initialValue: _routingGroup,
                                        decoration: InputDecoration(labelText: t.routingGroupDefault),
                                        items: <DropdownMenuItem<String>>[
                                          DropdownMenuItem(
                                            value: '',
                                            child: Text(t.routingGroupNoKeyDefault, overflow: TextOverflow.ellipsis),
                                          ),
                                          if (_routingGroup.isNotEmpty && !_groups.any((group) => group['name'] == _routingGroup)) DropdownMenuItem(value: _routingGroup, child: Text(_routingGroup)),
                                          for (final group in _groups)
                                            DropdownMenuItem(
                                              value: group['name'] as String,
                                              child: Text('${group['name']}', overflow: TextOverflow.ellipsis),
                                            ),
                                        ],
                                        onChanged: (value) => setState(() => _routingGroup = value ?? ''),
                                      ),
                                    ),
                                  ],
                                ),
                              ],
                            ),
                    ),
                    if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
                  ],
                ),
              ),
            ),
            Divider(height: 1, color: palette.border),
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 12, 24, 16),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: <Widget>[
                  TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
                  const SizedBox(width: AppSpacing.sm),
                  FilledButton(onPressed: _saving ? null : _submit, child: Text(widget.existing == null ? t.createKey : t.save)),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// A titled block of the key form, separated by space and a thin rule.
class _FormSection extends StatelessWidget {
  const _FormSection({required this.title, required this.child, this.trailing});
  final String title;
  final Widget child;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.lg),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Divider(height: 1, color: palette.border),
          const SizedBox(height: AppSpacing.sm),
          Row(
            children: <Widget>[
              Expanded(
                child: Text(
                  title.toUpperCase(),
                  style: TextStyle(fontSize: 11.5, letterSpacing: 0.8, fontWeight: FontWeight.w600, color: palette.textMuted),
                ),
              ),
              ?trailing,
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          child,
        ],
      ),
    );
  }
}

/// Title, one muted line of help, and a switch on the right.
class _ToggleRow extends StatelessWidget {
  const _ToggleRow({required this.title, required this.hint, required this.value, this.onChanged, this.trailing});
  final String title;
  final String hint;
  final bool value;
  final ValueChanged<bool>? onChanged;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return InkWell(
      borderRadius: BorderRadius.circular(8),
      onTap: onChanged == null ? null : () => onChanged!(!value),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Row(
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(
                    title,
                    style: TextStyle(fontWeight: FontWeight.w500, color: onChanged == null ? palette.textMuted : palette.text),
                  ),
                  const SizedBox(height: 2),
                  Text(hint, style: TextStyle(fontSize: 12, color: palette.textMuted, height: 1.35)),
                ],
              ),
            ),
            ?trailing,
            const SizedBox(width: AppSpacing.sm),
            Switch(value: value, onChanged: onChanged),
          ],
        ),
      ),
    );
  }
}

/// A selectable choice with a one-line explanation.

String? _paceText(AppL10n t, RouterKey key) => switch (key.paceState) {
  'ahead' => t.paceAhead,
  'on_track' => t.paceOnTrack,
  'tight' => t.paceTight(formatUsd(key.paceForecast ?? 0)),
  'over' => t.paceOver,
  _ => null,
};

/// What a key may use: the three kinds of models and, for paid models, the
/// spending limit. Shared by the key form and the connect-an-app screen.
class KeyAccessFields extends StatelessWidget {
  const KeyAccessFields({
    super.key,
    required this.free,
    required this.plan,
    required this.paid,
    required this.budget,
    required this.period,
    this.rpm,
    required this.tokens,
    required this.tokenPeriod,
    required this.maxPrice,
    required this.onFree,
    required this.onPlan,
    required this.onPaid,
    required this.onPeriod,
    required this.onTokenPeriod,
    this.pool = false,
    this.onPool,
    this.kinds,
  });

  /// Optional chips narrowing which kinds of model (chat, images, …) the key may use.
  final Widget? kinds;

  final bool free;
  final bool plan;
  final bool paid;
  final TextEditingController budget;
  final String period;
  final TextEditingController? rpm;
  final TextEditingController tokens;
  final String tokenPeriod;
  final TextEditingController maxPrice;
  final ValueChanged<bool> onFree;
  final ValueChanged<bool> onPlan;
  final ValueChanged<bool> onPaid;
  final ValueChanged<String> onPeriod;
  final ValueChanged<String> onTokenPeriod;

  /// Whether the key may also use pool partners' free models; the choice is
  /// shown only while in a pool (onPool set).
  final bool pool;
  final ValueChanged<bool>? onPool;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final hint = TextStyle(fontSize: 12, color: palette.textMuted, height: 1.4);
    final label = TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: palette.textSoft);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        _ToggleRow(title: t.kindFree, hint: t.kindFreeHint, value: free, onChanged: onFree),
        _ToggleRow(title: t.kindPlan, hint: t.kindPlanHint, value: plan, onChanged: onPlan),
        _ToggleRow(title: t.kindPaid, hint: t.kindPaidHint, value: paid, onChanged: onPaid),
        if (onPool != null)
          _ToggleRow(
            title: t.keyPoolLabel,
            hint: t.keyPoolHint,
            value: pool && free,
            onChanged: free ? onPool : null,
            trailing: IconButton(tooltip: t.poolInfoTooltip, icon: const Icon(Icons.info_outline_rounded, size: 18), onPressed: () => showPoolTrustInfo(context)),
          ),
        if (kinds != null) ...<Widget>[const SizedBox(height: AppSpacing.md), Text(t.keyKindsTitle, style: label), const SizedBox(height: AppSpacing.xs), kinds!],
        const SizedBox(height: AppSpacing.lg),
        Row(
          children: <Widget>[
            Text(t.keyLimitsTitle, style: label),
            const SizedBox(width: 6),
            Tooltip(
              message: t.keyNoLimitHint,
              child: Icon(Icons.info_outline_rounded, size: 15, color: palette.textMuted),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        // Every key: how many requests a minute.
        if (rpm case final field?) _LimitRow(label: t.keyRateLimit, controller: field, unit: t.keyRequestsPerMinute),
        // Free and subscription models: a token limit.
        if (free || plan) _LimitRow(label: t.keyTokenLimit, controller: tokens, unit: t.keyTokensPer, period: tokenPeriod, onPeriod: onTokenPeriod),
        // Paid models: a spending limit and a maximum price per million tokens.
        if (paid) ...<Widget>[
          _LimitRow(label: t.keyBudgetTitle, controller: budget, prefix: r'$ ', decimal: true, unit: t.spendingLimitPer, period: period, onPeriod: onPeriod),
          _LimitRow(label: t.keyMaxPriceTitle, controller: maxPrice, decimal: true, unit: t.keyMaxPricePerMillion),
        ],
        if (paid || free || plan) ...<Widget>[const SizedBox(height: 4), Text(paid ? (free || plan ? t.budgetFallsBack : t.budgetStops) : t.keyTokenHint, style: hint)],
      ],
    );
  }
}

/// "Label  [ value ]  unit  [ period ]" for one limit.
class _LimitRow extends StatelessWidget {
  const _LimitRow({required this.label, required this.controller, required this.unit, this.prefix, this.decimal = false, this.period, this.onPeriod});

  final String label;
  final TextEditingController controller;
  final String unit;
  final String? prefix;
  final bool decimal;
  final String? period;
  final ValueChanged<String>? onPeriod;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.xs),
      child: Row(
        children: <Widget>[
          Expanded(
            flex: 4,
            child: Text(label, style: TextStyle(color: palette.textSoft, fontSize: 13)),
          ),
          SizedBox(
            width: 110,
            child: TextField(
              controller: controller,
              keyboardType: TextInputType.numberWithOptions(decimal: decimal),
              textAlign: TextAlign.right,
              decoration: InputDecoration(prefixText: prefix, isDense: true, hintText: '—'),
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            flex: 5,
            child: Row(
              children: <Widget>[
                Flexible(
                  child: Text(
                    unit,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: palette.textMuted, fontSize: 12.5),
                  ),
                ),
                if (period != null && onPeriod != null) ...<Widget>[
                  const SizedBox(width: 4),
                  DropdownButton<String>(
                    value: period,
                    isDense: true,
                    underline: const SizedBox.shrink(),
                    style: TextStyle(color: palette.text, fontSize: 12.5),
                    onChanged: (value) => onPeriod!(value ?? period!),
                    items: <DropdownMenuItem<String>>[
                      DropdownMenuItem<String>(value: 'daily', child: Text(t.periodDaily)),
                      DropdownMenuItem<String>(value: 'monthly', child: Text(t.periodMonthly)),
                      DropdownMenuItem<String>(value: 'total', child: Text(t.periodTotal)),
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
