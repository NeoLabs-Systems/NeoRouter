import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_model_picker.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

/// "Free · Plans · Paid" for the kinds of models a key may use.
String keyKindsLabel(AppL10n t, RouterKey key) =>
    <String>[if (key.allowFree) t.kindFreeShort, if (key.allowPlan) t.kindPlanShort, if (key.allowPaid) t.kindPaidShort].join(' · ');

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

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final results = await Future.wait(<Future<Object>>[widget.controller.api.keys(), widget.controller.api.profiles()]);
      setState(() {
        _keys = results[0] as List<RouterKey>;
        _profiles = results[1] as List<RoutingProfile>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _open([RouterKey? key]) async {
    final secret = await showDialog<String>(
      context: context,
      builder: (_) => _KeyForm(controller: widget.controller, profiles: _profiles, existing: key),
    );
    await _load();
    if (secret != null && mounted) await _showSecret(secret);
  }

  Future<void> _showSecret(String secret) async {
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

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final active = _keys.where((key) => !key.revoked && !key.isConnectedApp).toList();
    final apps = _keys.where((key) => !key.revoked && key.isConnectedApp).toList();
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
                    title: t.noKeysTitle,
                    message: t.noKeysMessage,
                    action: canCreate
                        ? FilledButton.icon(onPressed: () => _open(), icon: const Icon(Icons.add_rounded, size: 18), label: Text(t.createKey))
                        : null,
                  ),
                )
              else
                SettingsCardList(children: <Widget>[for (final key in active) row(key)]),
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
    this.showCreator = false,
    this.canEdit = true,
    this.canDelete = true,
  });

  final RouterKey routerKey;
  final List<RoutingProfile> profiles;
  final ValueChanged<String> onAction;
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
      if (!key.revoked && canEdit) PopupMenuItem<String>(value: 'edit', child: Text(t.edit)),
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
                    const SizedBox(width: 10),
                    Text('${key.prefix}…', style: GoogleFonts.geistMono(fontSize: 12, color: palette.textMuted)),
                  ],
                ),
                const SizedBox(height: 3),
                Text(
                  subtitle,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 13, color: palette.textMuted),
                ),
                if (key.tokenLimit != null && !key.revoked) ...<Widget>[
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
                if (budget != null && !key.revoked) ...<Widget>[
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
  late final TextEditingController _tokens = TextEditingController(text: widget.existing?.tokenLimit?.toString() ?? '');
  late String _tokenPeriod = widget.existing?.tokenPeriod ?? 'monthly';
  late final TextEditingController _allowed = TextEditingController(text: widget.existing?.allowedModels.join('\n') ?? '');
  late bool _free = widget.existing?.allowFree ?? true;
  late bool _plan = widget.existing?.allowPlan ?? true;
  late bool _paid = widget.existing?.allowPaid ?? false;
  late String _period = widget.existing?.budgetPeriod ?? 'monthly';
  late String _profile = widget.existing?.profile ?? 'balanced';
  bool _advanced = false;
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _budget.dispose();
    _rpm.dispose();
    _tokens.dispose();
    _allowed.dispose();
    super.dispose();
  }

  // Picked models join the typed lines; patterns like "groq/*" stay as they are.
  Future<void> _pickAllowed() async {
    final lines = _allowed.text.split('\n').map((line) => line.trim()).where((line) => line.isNotEmpty).toList();
    final picked = await showModelPicker(context, controller: widget.controller, selected: lines.where((line) => !line.contains('*')).toList(), multi: true);
    if (picked == null) return;
    final patterns = lines.where((line) => line.contains('*'));
    setState(() => _allowed.text = <String>{...patterns, ...picked}.join('\n'));
  }

  Future<void> _submit() async {
    if (!_free && !_plan && !_paid) {
      setState(() => _error = AppL10n.of(context).pickOneKind);
      return;
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
      'tokenLimit': _free || _plan ? int.tryParse(_tokens.text.trim().replaceAll(RegExp(r'[^0-9]'), '')) : null,
      'tokenPeriod': _tokenPeriod,
      'allowedModels': _allowed.text.split('\n').map((line) => line.trim()).where((line) => line.isNotEmpty).toList(),
      'profile': _profile,
    };
    try {
      if (widget.existing != null) {
        await widget.controller.api.updateKey(widget.existing!.id, body);
        if (mounted) Navigator.of(context).pop();
      } else {
        final created = await widget.controller.api.createKey(body);
        if (mounted) Navigator.of(context).pop(created.secret);
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
    return AlertDialog(
      title: Text(widget.existing == null ? t.createKey : t.editKey),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(
                controller: _name,
                autofocus: true,
                decoration: InputDecoration(labelText: t.keyName, hintText: t.keyNameHint),
              ),
              const SizedBox(height: AppSpacing.lg),
              KeyAccessFields(
                free: _free,
                plan: _plan,
                paid: _paid,
                budget: _budget,
                period: _period,
                rpm: _rpm,
                tokens: _tokens,
                tokenPeriod: _tokenPeriod,
                onFree: (value) => setState(() => _free = value),
                onPlan: (value) => setState(() => _plan = value),
                onPaid: (value) => setState(() => _paid = value),
                onPeriod: (value) => setState(() => _period = value),
                onTokenPeriod: (value) => setState(() => _tokenPeriod = value),
              ),
              const SizedBox(height: AppSpacing.sm),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _advanced = !_advanced),
                  icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.advancedOptions),
                ),
              ),
              if (_advanced) ...<Widget>[
                DropdownButtonFormField<String>(
                  initialValue: widget.profiles.any((item) => item.id == _profile) ? _profile : 'balanced',
                  decoration: InputDecoration(labelText: t.defaultProfile),
                  items: <DropdownMenuItem<String>>[
                    for (final profile in widget.profiles)
                      DropdownMenuItem<String>(value: profile.id, child: Text(profile.id == 'balanced' ? t.presetBalancedDefault : profileLabel(t, profile))),
                  ],
                  onChanged: (value) => setState(() => _profile = value ?? _profile),
                ),
                const SizedBox(height: AppSpacing.sm),
                TextField(
                  controller: _allowed,
                  minLines: 2,
                  maxLines: 5,
                  decoration: InputDecoration(labelText: t.allowedModels, hintText: 'groq/*\nopenrouter/*:free'),
                ),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(onPressed: _pickAllowed, icon: const Icon(Icons.checklist_rounded, size: 17), label: Text(t.pickModels)),
                ),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _saving ? null : _submit, child: Text(widget.existing == null ? t.createKey : t.save)),
      ],
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
    required this.rpm,
    required this.tokens,
    required this.tokenPeriod,
    required this.onFree,
    required this.onPlan,
    required this.onPaid,
    required this.onPeriod,
    required this.onTokenPeriod,
  });

  final bool free;
  final bool plan;
  final bool paid;
  final TextEditingController budget;
  final String period;
  final TextEditingController rpm;
  final TextEditingController tokens;
  final String tokenPeriod;
  final ValueChanged<bool> onFree;
  final ValueChanged<bool> onPlan;
  final ValueChanged<bool> onPaid;
  final ValueChanged<String> onPeriod;
  final ValueChanged<String> onTokenPeriod;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final label = TextStyle(fontWeight: FontWeight.w600, color: palette.text);
    final hint = TextStyle(fontSize: 12, color: palette.textMuted, height: 1.4);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(t.keyAccessTitle, style: label),
        const SizedBox(height: AppSpacing.xs),
        _KindCheck(title: t.kindFree, description: t.kindFreeHint, value: free, onChanged: onFree),
        _KindCheck(title: t.kindPlan, description: t.kindPlanHint, value: plan, onChanged: onPlan),
        _KindCheck(title: t.kindPaid, description: t.kindPaidHint, value: paid, onChanged: onPaid),
        const SizedBox(height: AppSpacing.md),
        Text(t.keyLimitsTitle, style: label),
        const SizedBox(height: 2),
        Text(t.keyNoLimitHint, style: hint),
        const SizedBox(height: AppSpacing.sm),
        // Every key: how many requests a minute.
        _LimitRow(label: t.keyRateLimit, controller: rpm, unit: t.keyRequestsPerMinute),
        // Paid models: a spending limit.
        if (paid) ...<Widget>[
          const SizedBox(height: AppSpacing.sm),
          _LimitRow(label: t.keyBudgetTitle, controller: budget, prefix: r'$ ', decimal: true, unit: t.spendingLimitPer, period: period, onPeriod: onPeriod),
          const SizedBox(height: 4),
          Text(free || plan ? t.budgetFallsBack : t.budgetStops, style: hint),
        ],
        // Free and subscription models: a token limit.
        if (free || plan) ...<Widget>[
          const SizedBox(height: AppSpacing.sm),
          _LimitRow(label: t.keyTokenLimit, controller: tokens, unit: t.keyTokensPer, period: tokenPeriod, onPeriod: onTokenPeriod),
          const SizedBox(height: 4),
          Text(paid ? t.keyTokenFallsBack : t.keyTokenHint, style: hint),
        ],
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
    return Wrap(
      spacing: AppSpacing.sm,
      runSpacing: AppSpacing.xs,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: <Widget>[
        SizedBox(width: 120, child: Text(label, style: TextStyle(color: palette.textSoft))),
        SizedBox(
          width: 120,
          child: TextField(
            controller: controller,
            keyboardType: TextInputType.numberWithOptions(decimal: decimal),
            decoration: InputDecoration(prefixText: prefix, isDense: true, hintText: '—'),
          ),
        ),
        Text(unit, style: TextStyle(color: palette.textSoft)),
        if (period != null && onPeriod != null)
          DropdownButton<String>(
            value: period,
            onChanged: (value) => onPeriod!(value ?? period!),
            items: <DropdownMenuItem<String>>[
              DropdownMenuItem<String>(value: 'daily', child: Text(t.periodDaily)),
              DropdownMenuItem<String>(value: 'monthly', child: Text(t.periodMonthly)),
              DropdownMenuItem<String>(value: 'total', child: Text(t.periodTotal)),
            ],
          ),
      ],
    );
  }
}

/// One kind of model a key may use: a checkbox card.
class _KindCheck extends StatelessWidget {
  const _KindCheck({required this.title, required this.description, required this.value, required this.onChanged});

  final String title;
  final String description;
  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Material(
        color: value ? palette.accentSoft : palette.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(AppRadius.card),
          side: BorderSide(color: value ? palette.accent.withValues(alpha: 0.5) : palette.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: () => onChanged(!value),
          child: Padding(
            padding: const EdgeInsets.fromLTRB(8, 10, 14, 10),
            child: Row(
              children: <Widget>[
                Checkbox(value: value, onChanged: (next) => onChanged(next ?? false)),
                const SizedBox(width: 4),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(
                        title,
                        style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
                      ),
                      const SizedBox(height: 2),
                      Text(description, style: TextStyle(fontSize: 12.5, color: palette.textSoft, height: 1.35)),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
