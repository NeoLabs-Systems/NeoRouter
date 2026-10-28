import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

String keyModeLabel(AppL10n t, String mode) => switch (mode) {
      'free_only' => t.modeFreeOnly,
      'paid' => t.modePaid,
      _ => t.modeBoth,
    };

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
          width: 520,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              Text(t.keySecretBody, style: TextStyle(color: palette.warning)),
              const SizedBox(height: AppSpacing.md),
              Container(
                padding: const EdgeInsets.all(AppSpacing.sm),
                decoration: BoxDecoration(color: palette.surfaceMuted, borderRadius: BorderRadius.circular(AppRadius.card)),
                child: SelectableText(secret, style: const TextStyle(fontFamily: 'monospace', fontSize: 13)),
              ),
            ],
          ),
        ),
        actions: <Widget>[
          OutlinedButton.icon(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: secret));
              showSnack(context, t.copied);
            },
            icon: const Icon(Icons.copy_rounded, size: 16),
            label: Text(t.copy),
          ),
          FilledButton(onPressed: () => Navigator.of(dialogContext).pop(), child: Text(t.close)),
        ],
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
    return PageScaffold(
      header: ScreenHeader(
        title: t.keysTitle,
        description: t.keysDescription,
        trailing: FilledButton.icon(onPressed: _loading ? null : () => _open(), icon: const Icon(Icons.add, size: 18), label: Text(t.createKey)),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _keys.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.key_outlined, title: t.noKeysTitle, message: t.noKeysMessage))
              : Column(
                  children: <Widget>[
                    for (final key in _keys) ...<Widget>[
                      _KeyCard(
                        routerKey: key,
                        profiles: _profiles,
                        onEdit: key.revoked ? null : () => _open(key),
                        onRevoke: key.revoked ? null : () => _revoke(key),
                        onDelete: () => _delete(key),
                      ),
                      const SizedBox(height: AppSpacing.md),
                    ],
                  ],
                ),
        ),
      ],
    );
  }
}

class _KeyCard extends StatelessWidget {
  const _KeyCard({required this.routerKey, required this.profiles, required this.onEdit, required this.onRevoke, required this.onDelete});

  final RouterKey routerKey;
  final List<RoutingProfile> profiles;
  final VoidCallback? onEdit;
  final VoidCallback? onRevoke;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final key = routerKey;
    final profile = profiles.where((item) => item.id == key.profile).firstOrNull;
    final budget = key.budgetUsd;
    return SectionCard(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(key.name, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 2),
                    Text('${key.prefix}_••••', style: TextStyle(color: palette.textMuted, fontFamily: 'monospace', fontSize: 12)),
                  ],
                ),
              ),
              if (onEdit != null) IconButton(tooltip: t.edit, onPressed: onEdit, icon: const Icon(Icons.edit_outlined, size: 18)),
              if (onRevoke != null) IconButton(tooltip: t.revoke, onPressed: onRevoke, icon: const Icon(Icons.block, size: 18)),
              IconButton(tooltip: t.delete, onPressed: onDelete, icon: const Icon(Icons.delete_outline, size: 18)),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              if (key.revoked) MetaPill(label: t.revoked, tone: PillTone.error),
              MetaPill(label: keyModeLabel(t, key.mode), tone: key.mode == 'free_only' ? PillTone.success : PillTone.accent),
              MetaPill(label: 'auto → ${profile == null ? key.profile : profileLabel(t, profile)}', icon: Icons.alt_route_outlined),
              if (key.rpmLimit != null) MetaPill(label: '${key.rpmLimit} RPM'),
              if (key.allowedModels.isNotEmpty) MetaPill(label: key.allowedModels.join(', ')),
              MetaPill(label: t.lastUsed(key.lastUsedAt == null ? t.never : formatTime(DateTime.parse(key.lastUsedAt!).toLocal()))),
            ],
          ),
          const SizedBox(height: AppSpacing.md),
          if (budget != null) ...<Widget>[
            Row(
              children: <Widget>[
                Expanded(child: Text(t.spentOf(formatUsd(key.spentUsd), formatUsd(budget)), style: const TextStyle(fontWeight: FontWeight.w600))),
                if (key.periodResetsAt != null)
                  Text(t.resetsAt(formatTime(DateTime.parse(key.periodResetsAt!).toLocal())), style: TextStyle(color: palette.textMuted, fontSize: 12)),
              ],
            ),
            const SizedBox(height: 6),
            UsageBar(fraction: budget == 0 ? 1 : key.spentUsd / budget),
          ] else
            Text(t.spentNoBudget(formatUsd(key.spentUsd)), style: TextStyle(color: palette.textSoft)),
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
  late final TextEditingController _allowed = TextEditingController(text: widget.existing?.allowedModels.join('\n') ?? '');
  late String _mode = widget.existing?.mode ?? 'free_only';
  late String _period = widget.existing?.budgetPeriod ?? 'monthly';
  late String _profile = widget.existing?.profile ?? 'balanced';
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _budget.dispose();
    _rpm.dispose();
    _allowed.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    final body = <String, Object?>{
      'name': _name.text.trim(),
      'mode': _mode,
      'budgetUsd': double.tryParse(_budget.text.trim().replaceAll(',', '.')),
      'budgetPeriod': _period,
      'rpmLimit': int.tryParse(_rpm.text.trim()),
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
    final palette = neoRouterPaletteOf(context);
    return AlertDialog(
      title: Text(widget.existing == null ? t.createKey : t.editKey),
      content: SizedBox(
        width: 500,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              TextField(controller: _name, autofocus: true, decoration: InputDecoration(labelText: t.keyName)),
              const SizedBox(height: AppSpacing.md),
              Text(t.keyMode.toUpperCase(), style: sectionEyebrowStyle(palette)),
              RadioGroup<String>(
                groupValue: _mode,
                onChanged: (value) => setState(() => _mode = value ?? _mode),
                child: Column(
                  children: <Widget>[
                    for (final mode in <String>['free_only', 'paid', 'both'])
                      RadioListTile<String>(contentPadding: EdgeInsets.zero, value: mode, title: Text(keyModeLabel(t, mode))),
                  ],
                ),
              ),
              if (_mode != 'free_only') ...<Widget>[
                Row(
                  children: <Widget>[
                    Expanded(
                      child: TextField(
                        controller: _budget,
                        keyboardType: const TextInputType.numberWithOptions(decimal: true),
                        decoration: InputDecoration(labelText: t.budget, prefixText: r'$ '),
                      ),
                    ),
                    const SizedBox(width: AppSpacing.sm),
                    DropdownButton<String>(
                      value: _period,
                      underline: const SizedBox.shrink(),
                      items: <DropdownMenuItem<String>>[
                        DropdownMenuItem<String>(value: 'total', child: Text(t.periodTotal)),
                        DropdownMenuItem<String>(value: 'monthly', child: Text(t.periodMonthly)),
                        DropdownMenuItem<String>(value: 'daily', child: Text(t.periodDaily)),
                      ],
                      onChanged: (value) => setState(() => _period = value ?? _period),
                    ),
                  ],
                ),
                const SizedBox(height: AppSpacing.sm),
              ],
              DropdownButtonFormField<String>(
                initialValue: widget.profiles.any((item) => item.id == _profile) ? _profile : 'balanced',
                decoration: InputDecoration(labelText: t.defaultProfile),
                items: <DropdownMenuItem<String>>[
                  for (final profile in widget.profiles) DropdownMenuItem<String>(value: profile.id, child: Text(profileLabel(t, profile))),
                ],
                onChanged: (value) => setState(() => _profile = value ?? _profile),
              ),
              const SizedBox(height: AppSpacing.sm),
              TextField(controller: _rpm, keyboardType: TextInputType.number, decoration: InputDecoration(labelText: t.rpmLimit)),
              const SizedBox(height: AppSpacing.sm),
              TextField(
                controller: _allowed,
                minLines: 2,
                maxLines: 5,
                decoration: InputDecoration(labelText: t.allowedModels, hintText: 'groq/*\nopenrouter/*:free'),
              ),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        OutlinedButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _saving ? null : _submit, child: Text(widget.existing == null ? t.create : t.save)),
      ],
    );
  }
}
