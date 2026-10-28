import 'package:flutter/material.dart';
import 'package:url_launcher/url_launcher.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

const List<String> _categoryOrder = <String>['free', 'aggregator', 'paid', 'local', 'custom', 'community'];

String categoryLabel(AppL10n t, String category) => switch (category) {
      'free' => t.categoryFree,
      'aggregator' => t.categoryAggregator,
      'paid' => t.categoryPaid,
      'local' => t.categoryLocal,
      'custom' => t.categoryCustom,
      _ => t.categoryCommunity,
    };

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

  Future<void> _add() async {
    final definition = await showDialog<ProviderDefinition>(
      context: context,
      builder: (_) => _ProviderGallery(definitions: _definitions),
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

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.providersTitle,
        description: t.providersDescription,
        trailing: FilledButton.icon(
          onPressed: _definitions.isEmpty ? null : _add,
          icon: const Icon(Icons.add, size: 18),
          label: Text(t.addProvider),
        ),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _accounts.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.hub_outlined, title: t.noProvidersTitle, message: t.noProvidersMessage))
              : LayoutBuilder(
                  builder: (context, constraints) {
                    final columns = constraints.maxWidth >= 1100 ? 3 : constraints.maxWidth >= 700 ? 2 : 1;
                    final width = (constraints.maxWidth - (columns - 1) * AppSpacing.md) / columns;
                    return Wrap(
                      spacing: AppSpacing.md,
                      runSpacing: AppSpacing.md,
                      children: <Widget>[
                        for (final account in _accounts)
                          SizedBox(
                            width: width,
                            child: _AccountCard(
                              account: account,
                              busy: _busy.contains(account.id),
                              onValidate: () => _run(account.id, () async {
                                final result = await widget.controller.api.validateAccount(account.id);
                                return (result['validation'] as Map?)?['message'] as String? ?? t.statusHealthy;
                              }),
                              onSync: () => _run(account.id, () async {
                                final result = await widget.controller.api.syncAccount(account.id);
                                return t.syncedModels('${(result['sync'] as Map?)?['models'] ?? 0}');
                              }),
                              onQuota: () => _run(account.id, () async {
                                final result = await widget.controller.api.probeQuota(account.id);
                                final items = (result['quota'] as List).cast<Map>();
                                if (items.isEmpty) return t.quotaProbeEmpty;
                                return items.map((item) => t.quotaProbeResult('${item['metric']}', '${item['remaining']}')).join(' · ');
                              }),
                              onToggle: (enabled) => _run(account.id, () async {
                                await widget.controller.api.updateAccount(account.id, <String, Object?>{'enabled': enabled});
                                return null;
                              }),
                              onEdit: () => _edit(account),
                              onDelete: () async {
                                final confirmed = await confirmAction(
                                  context,
                                  title: t.removeProvider,
                                  message: t.removeProviderConfirm(account.label),
                                  confirmLabel: t.delete,
                                  cancelLabel: t.cancel,
                                );
                                if (confirmed) {
                                  await _run(account.id, () async {
                                    await widget.controller.api.deleteAccount(account.id);
                                    return null;
                                  });
                                }
                              },
                            ),
                          ),
                      ],
                    );
                  },
                ),
        ),
      ],
    );
  }
}

class _AccountCard extends StatelessWidget {
  const _AccountCard({
    required this.account,
    required this.busy,
    required this.onValidate,
    required this.onSync,
    required this.onQuota,
    required this.onToggle,
    required this.onEdit,
    required this.onDelete,
  });

  final ProviderAccount account;
  final bool busy;
  final VoidCallback onValidate;
  final VoidCallback onSync;
  final VoidCallback onQuota;
  final ValueChanged<bool> onToggle;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final (statusLabel, tone) = switch (account.status) {
      'healthy' => (t.statusHealthy, PillTone.success),
      'invalid' => (t.statusInvalid, PillTone.error),
      'disabled' => (t.statusDisabled, PillTone.error),
      'error' => (t.statusError, PillTone.warning),
      _ => (t.statusUnknown, PillTone.neutral),
    };
    return SectionCard(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Row(
            children: <Widget>[
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Text(account.label, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
                    const SizedBox(height: 2),
                    Text('${account.slug}/…  ·  ${account.providerLabel}', overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textMuted, fontSize: 12, fontFamily: 'monospace')),
                  ],
                ),
              ),
              Switch(value: account.enabled, onChanged: busy ? null : onToggle),
            ],
          ),
          const SizedBox(height: AppSpacing.sm),
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              MetaPill(label: statusLabel, tone: tone),
              MetaPill(
                label: account.billingMode == 'free_tier' ? t.free : t.paid,
                tone: account.billingMode == 'free_tier' ? PillTone.success : PillTone.accent,
                icon: account.billingMode == 'free_tier' ? Icons.savings_outlined : Icons.payments_outlined,
              ),
              MetaPill(label: t.modelsCount('${account.modelCount}', '${account.freeModelCount}')),
              if (account.keyPreview != null) MetaPill(label: account.keyPreview!, icon: Icons.key_outlined),
            ],
          ),
          if (account.statusMessage != null && account.status != 'healthy') ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            Text(account.statusMessage!, maxLines: 3, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.warning, fontSize: 12)),
          ],
          const SizedBox(height: AppSpacing.sm),
          Row(
            children: <Widget>[
              if (busy) const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2)) else ...<Widget>[
                _ActionIcon(tooltip: t.revalidate, icon: Icons.verified_user_outlined, onTap: onValidate),
                _ActionIcon(tooltip: t.syncModels, icon: Icons.sync, onTap: onSync),
                _ActionIcon(tooltip: t.probeQuota, icon: Icons.account_balance_wallet_outlined, onTap: onQuota),
                _ActionIcon(tooltip: t.editProvider, icon: Icons.edit_outlined, onTap: onEdit),
              ],
              const Spacer(),
              _ActionIcon(tooltip: t.removeProvider, icon: Icons.delete_outline, onTap: busy ? null : onDelete),
            ],
          ),
        ],
      ),
    );
  }
}

class _ActionIcon extends StatelessWidget {
  const _ActionIcon({required this.tooltip, required this.icon, required this.onTap});

  final String tooltip;
  final IconData icon;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    return IconButton(tooltip: tooltip, visualDensity: VisualDensity.compact, icon: Icon(icon, size: 18), onPressed: onTap);
  }
}

class _ProviderGallery extends StatefulWidget {
  const _ProviderGallery({required this.definitions});

  final List<ProviderDefinition> definitions;

  @override
  State<_ProviderGallery> createState() => _ProviderGalleryState();
}

class _ProviderGalleryState extends State<_ProviderGallery> {
  String _query = '';

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final query = _query.toLowerCase();
    final matches = widget.definitions.where((item) => query.isEmpty || item.label.toLowerCase().contains(query) || item.id.contains(query) || item.description.toLowerCase().contains(query));
    return AlertDialog(
      title: Text(t.chooseProvider),
      content: SizedBox(
        width: 720,
        height: 560,
        child: Column(
          children: <Widget>[
            TextField(
              autofocus: true,
              decoration: InputDecoration(prefixIcon: const Icon(Icons.search), hintText: t.searchProviders),
              onChanged: (value) => setState(() => _query = value),
            ),
            const SizedBox(height: AppSpacing.md),
            Expanded(
              child: ListView(
                children: <Widget>[
                  for (final category in _categoryOrder)
                    if (matches.any((item) => item.category == category)) ...<Widget>[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(4, 8, 4, 8),
                        child: Text(categoryLabel(t, category).toUpperCase(), style: sectionEyebrowStyle(palette)),
                      ),
                      Wrap(
                        spacing: 8,
                        runSpacing: 8,
                        children: <Widget>[
                          for (final definition in matches.where((item) => item.category == category))
                            SizedBox(
                              width: 220,
                              child: Material(
                                color: palette.surfaceMuted.withValues(alpha: 0.6),
                                borderRadius: BorderRadius.circular(AppRadius.card),
                                child: InkWell(
                                  borderRadius: BorderRadius.circular(AppRadius.card),
                                  onTap: () => Navigator.of(context).pop(definition),
                                  child: Padding(
                                    padding: const EdgeInsets.all(AppSpacing.sm),
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: <Widget>[
                                        Row(
                                          children: <Widget>[
                                            Expanded(child: Text(definition.label, overflow: TextOverflow.ellipsis, style: const TextStyle(fontWeight: FontWeight.w700))),
                                            if (definition.freeTierAvailable || definition.hasAlwaysFreeModels) Icon(Icons.savings_outlined, size: 14, color: palette.success),
                                          ],
                                        ),
                                        const SizedBox(height: 4),
                                        Text(definition.description, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.textSoft, fontSize: 12)),
                                      ],
                                    ),
                                  ),
                                ),
                              ),
                            ),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.sm),
                    ],
                ],
              ),
            ),
          ],
        ),
      ),
      actions: <Widget>[OutlinedButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel))],
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
    for (final field in widget.definition.fields)
      field.key: TextEditingController(text: '${(widget.account?.raw['options'] as Map?)?[field.key] ?? ''}'),
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
  Map<String, dynamic>? _probe;
  String? _error;

  bool get _editing => widget.account != null;
  bool get _custom => widget.definition.editableBaseUrl;

  @override
  void dispose() {
    for (final controller in <TextEditingController>[_label, _slug, _secret, _baseUrl, _authHeader, _modelsPath, _manualModels, _headers, ..._fields.values]) {
      controller.dispose();
    }
    super.dispose();
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
        'label': _label.text.trim(),
        'slug': _slug.text.trim(),
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
    return AlertDialog(
      title: Text(_editing ? t.editProvider : definition.label),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              if (definition.description.isNotEmpty) Text(definition.description, style: TextStyle(color: palette.textSoft)),
              if (definition.keyUrl != null) ...<Widget>[
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    onPressed: () => launchUrl(Uri.parse(definition.keyUrl!)),
                    icon: const Icon(Icons.open_in_new, size: 16),
                    label: Text(t.getKey),
                  ),
                ),
              ],
              if (definition.id == 'openrouter' && !_editing) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                FilledButton.icon(
                  onPressed: _connecting ? null : _connectOpenRouter,
                  icon: _connecting
                      ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.bolt_rounded, size: 18),
                  label: Text(t.connectOpenRouter),
                ),
                const SizedBox(height: 6),
                Text(t.connectOpenRouterHint, style: TextStyle(color: palette.textMuted, fontSize: 12)),
                const SizedBox(height: AppSpacing.sm),
                Row(
                  children: <Widget>[
                    Expanded(child: Divider(color: palette.border)),
                    Padding(padding: const EdgeInsets.symmetric(horizontal: 10), child: Text(t.orManualKey, style: TextStyle(color: palette.textMuted, fontSize: 12))),
                    Expanded(child: Divider(color: palette.border)),
                  ],
                ),
              ],
              const SizedBox(height: AppSpacing.sm),
              TextField(
                controller: _secret,
                obscureText: true,
                autofocus: !_editing && definition.id != 'openrouter',
                decoration: InputDecoration(labelText: _editing ? t.apiKeyKeep : definition.keyOptional ? t.apiKeyOptional : t.apiKey),
              ),
              for (final field in definition.fields) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                TextField(controller: _fields[field.key], decoration: InputDecoration(labelText: field.label, hintText: field.placeholder)),
              ],
              if (definition.editableBaseUrl) ...<Widget>[
                const SizedBox(height: AppSpacing.sm),
                TextField(controller: _baseUrl, decoration: InputDecoration(labelText: t.baseUrl, hintText: 'https://…/v1')),
                const SizedBox(height: AppSpacing.xs),
                Theme(
                  data: Theme.of(context).copyWith(dividerColor: Colors.transparent),
                  child: ExpansionTile(
                    tilePadding: EdgeInsets.zero,
                    childrenPadding: EdgeInsets.zero,
                    title: Text(t.advancedOptions, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                    children: <Widget>[
                      TextField(controller: _authHeader, decoration: InputDecoration(labelText: t.authHeaderLabel, hintText: 'x-api-key')),
                      const SizedBox(height: AppSpacing.sm),
                      TextField(controller: _headers, minLines: 1, maxLines: 4, decoration: InputDecoration(labelText: t.customHeaders, hintText: 'X-Org: my-team')),
                      const SizedBox(height: AppSpacing.sm),
                      TextField(controller: _modelsPath, decoration: InputDecoration(labelText: t.modelsPathLabel, hintText: '/models')),
                      const SizedBox(height: AppSpacing.sm),
                      TextField(
                        controller: _manualModels,
                        minLines: 2,
                        maxLines: 6,
                        decoration: InputDecoration(labelText: t.manualModels, helperText: t.manualModelsHint, helperMaxLines: 2),
                      ),
                      const SizedBox(height: AppSpacing.sm),
                    ],
                  ),
                ),
              ],
              const SizedBox(height: AppSpacing.sm),
              Align(
                alignment: Alignment.centerLeft,
                child: OutlinedButton.icon(
                  onPressed: _probing ? null : _runProbe,
                  icon: _probing
                      ? const SizedBox.square(dimension: 14, child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.travel_explore, size: 16),
                  label: Text(t.fetchModels),
                ),
              ),
              if (_probe != null) _ProbeResult(result: _probe!),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: <Widget>[
                  Expanded(child: TextField(controller: _label, decoration: InputDecoration(labelText: t.displayNameLabel))),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(
                    child: TextField(
                      controller: _slug,
                      onChanged: (_) => setState(() {}),
                      decoration: InputDecoration(labelText: t.slugLabel, helperText: t.slugHint(_slug.text.isEmpty ? '…' : _slug.text)),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: AppSpacing.md),
              Text(t.billingMode.toUpperCase(), style: sectionEyebrowStyle(palette)),
              const SizedBox(height: AppSpacing.xs),
              RadioGroup<String>(
                groupValue: _billing,
                onChanged: (value) => setState(() => _billing = value ?? _billing),
                child: Column(
                  children: <Widget>[
                    if (definition.freeTierAvailable)
                      RadioListTile<String>(contentPadding: EdgeInsets.zero, value: 'free_tier', title: Text(t.billingFree)),
                    RadioListTile<String>(contentPadding: EdgeInsets.zero, value: 'paid', title: Text(t.billingPaid)),
                  ],
                ),
              ),
              if (_billing == 'free_tier') Text(t.billingFreeWarning, style: TextStyle(color: palette.warning, fontSize: 12, height: 1.4)),
              if (definition.hasAlwaysFreeModels) ...<Widget>[
                const SizedBox(height: 6),
                Text(t.billingAlwaysFreeNote, style: TextStyle(color: palette.success, fontSize: 12)),
              ],
              if (definition.billingNote.isNotEmpty) ...<Widget>[
                const SizedBox(height: 6),
                Text(definition.billingNote, style: TextStyle(color: palette.textMuted, fontSize: 12, height: 1.4)),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.md), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        OutlinedButton(onPressed: _saving ? null : () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(
          onPressed: _saving ? null : _submit,
          child: _saving
              ? const SizedBox.square(dimension: 16, child: CircularProgressIndicator(strokeWidth: 2))
              : Text(_editing ? t.save : t.checkAndAdd),
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
    final chat = models.where((model) => model['kind'] == 'chat').toList();
    final status = validation['status'] as String? ?? 'unknown';
    final (label, tone) = switch (status) {
      'healthy' => (t.statusHealthy, PillTone.success),
      'invalid' => (t.statusInvalid, PillTone.error),
      _ => (t.statusError, PillTone.warning),
    };
    return Padding(
      padding: const EdgeInsets.only(top: AppSpacing.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: <Widget>[
              MetaPill(label: t.keyStatus(label), tone: tone),
              if (result['modelsError'] == null) MetaPill(label: t.modelsFound('${chat.length}'), tone: PillTone.accent),
            ],
          ),
          if (validation['message'] != null && status != 'healthy') ...<Widget>[
            const SizedBox(height: 6),
            Text('${validation['message']}', maxLines: 3, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.warning, fontSize: 12)),
          ],
          if (result['modelsError'] != null) ...<Widget>[
            const SizedBox(height: 6),
            Text(t.modelsFetchFailed('${result['modelsError']}'), maxLines: 3, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.error, fontSize: 12)),
          ],
          if (chat.isNotEmpty) ...<Widget>[
            const SizedBox(height: AppSpacing.sm),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 140),
              child: SingleChildScrollView(
                child: Wrap(
                  spacing: 4,
                  runSpacing: 4,
                  children: <Widget>[
                    for (final model in chat.take(80))
                      MetaPill(label: '${model['id']}', tone: model['freeHint'] == true ? PillTone.success : PillTone.neutral),
                  ],
                ),
              ),
            ),
          ],
        ],
      ),
    );
  }
}
